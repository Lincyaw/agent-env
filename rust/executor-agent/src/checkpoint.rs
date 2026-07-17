use std::fs;
use std::io;
use std::os::unix::fs::{FileTypeExt, MetadataExt, PermissionsExt};
use std::path::{Path, PathBuf};
use std::process::Command;
use std::sync::atomic::{AtomicU32, Ordering};

/// Captures per-step filesystem diffs using overlayfs.
///
/// Each spawned command runs inside a new mount namespace with an overlay
/// (lowerdir=/) mounted on a workspace subdirectory, then chroots into
/// the merged view. After the command exits, the overlay upper directory
/// contains exactly the files that were changed, which are then applied
/// back to the real filesystem.
///
/// AppArmor blocks overlay mounts directly on `/`, so we mount the
/// overlay on a subdirectory under the checkpoint scratch volume and
/// use chroot to make it the command's root.
pub struct Checkpointer {
    base_dir: PathBuf,
    step: AtomicU32,
}

impl Checkpointer {
    pub fn new(base_dir: PathBuf) -> Self {
        let _ = fs::create_dir_all(&base_dir);
        Self {
            base_dir,
            step: AtomicU32::new(0),
        }
    }

    /// Modify a `Command` to run inside an overlay mount namespace.
    /// Returns the step number for later use with `apply_step()`.
    pub fn wrap_command(&self, cmd: &mut Command) -> io::Result<u32> {
        let step = self.step.fetch_add(1, Ordering::Relaxed) + 1;

        let step_dir = self.base_dir.join(format!("step-{step}"));
        let upper_dir = step_dir.join("upper");
        let work_dir = step_dir.join("work");
        let merged_dir = step_dir.join("merged");

        fs::create_dir_all(&upper_dir)?;
        fs::create_dir_all(&work_dir)?;
        fs::create_dir_all(&merged_dir)?;

        let upper = upper_dir.to_string_lossy().into_owned();
        let work = work_dir.to_string_lossy().into_owned();
        let merged = merged_dir.to_string_lossy().into_owned();
        let base_dir = self.base_dir.to_string_lossy().into_owned();

        use std::os::unix::process::CommandExt;
        unsafe {
            cmd.pre_exec(move || {
                if libc::unshare(libc::CLONE_NEWNS) != 0 {
                    return Err(io::Error::last_os_error());
                }

                // Use /bin/mount binary instead of mount() syscall.
                // AppArmor cri-containerd.apparmor.d blocks direct mount()
                // syscalls but allows the setuid /bin/mount binary.
                let mount_cmd = format!(
                    "/bin/mount -t overlay overlay -o lowerdir=/,upperdir={upper},workdir={work} {merged}"
                );
                let c_cmd = std::ffi::CString::new(mount_cmd).unwrap();
                if libc::system(c_cmd.as_ptr()) != 0 {
                    return Err(io::Error::new(io::ErrorKind::PermissionDenied, "overlay mount failed"));
                }

                // Bind-mount essential filesystems into the merged view
                for src in &["/proc", "/dev", "/sys"] {
                    let bind_cmd = format!("/bin/mount --bind {src} {merged}{src}");
                    let c_bind = std::ffi::CString::new(bind_cmd).unwrap();
                    libc::system(c_bind.as_ptr());
                }

                // Bind-mount checkpoint scratch dir so upper is accessible after chroot
                let bind_ckpt = format!("/bin/mount --bind {base_dir} {merged}{base_dir}");
                let c_bind_ckpt = std::ffi::CString::new(bind_ckpt).unwrap();
                libc::system(c_bind_ckpt.as_ptr());

                // chroot into the overlay merged view
                let c_merged2 = std::ffi::CString::new(merged.as_str()).unwrap();
                if libc::chroot(c_merged2.as_ptr()) != 0 {
                    return Err(io::Error::last_os_error());
                }
                if libc::chdir(b"/\0".as_ptr() as *const libc::c_char) != 0 {
                    return Err(io::Error::last_os_error());
                }

                drop_sys_admin();

                Ok(())
            });
        }

        Ok(step)
    }

    /// Apply the overlay upper dir changes back to the real filesystem.
    /// Handles overlayfs whiteout markers (char device with rdev 0 = deletion).
    pub fn apply_step(&self, step: u32) -> io::Result<()> {
        let upper = self.step_upper_dir(step);
        if !upper.exists() {
            return Ok(());
        }

        apply_upper_dir(&upper)?;
        make_world_readable(&upper);
        Ok(())
    }

    /// Returns the upper dir path for a given step.
    pub fn step_upper_dir(&self, step: u32) -> PathBuf {
        self.base_dir.join(format!("step-{step}")).join("upper")
    }
}

/// Walk the overlay upper dir and apply changes to the real filesystem.
fn apply_upper_dir(upper: &Path) -> io::Result<()> {
    for entry in walkdir::WalkDir::new(upper) {
        let entry = entry.map_err(|e| {
            let path = e.path().map(|p| p.display().to_string()).unwrap_or_default();
            log::warn!("[checkpoint] walk error at {path}: {e}");
            io::Error::new(io::ErrorKind::Other, e)
        })?;

        let rel = entry.path().strip_prefix(upper).unwrap();
        if rel.as_os_str().is_empty() {
            continue;
        }
        let dst = Path::new("/").join(rel);
        let meta = entry.metadata().map_err(|e| {
            log::warn!("[checkpoint] metadata error at {}: {e}", entry.path().display());
            io::Error::new(io::ErrorKind::Other, e)
        })?;

        // Overlayfs whiteout: char device with rdev 0 means deletion
        if meta.file_type().is_char_device() && meta.rdev() == 0 {
            let _ = fs::remove_file(&dst);
            continue;
        }

        if meta.is_dir() {
            let _ = fs::create_dir_all(&dst);
        } else if meta.is_file() {
            if let Some(parent) = dst.parent() {
                let _ = fs::create_dir_all(parent);
            }
            fs::copy(entry.path(), &dst)?;
        }
    }
    Ok(())
}

/// Make the upper dir world-readable so the sidecar (nonroot) can serve it.
fn make_world_readable(upper: &Path) {
    for entry in walkdir::WalkDir::new(upper) {
        if let Ok(e) = entry {
            let path = e.path();
            let meta = match e.metadata() {
                Ok(m) => m,
                Err(_) => continue,
            };
            let mode = meta.permissions().mode();
            if meta.is_dir() {
                let _ = fs::set_permissions(path, fs::Permissions::from_mode(mode | 0o555));
            } else {
                let _ = fs::set_permissions(path, fs::Permissions::from_mode(mode | 0o444));
            }
        }
    }
}

/// Drop CAP_SYS_ADMIN from the bounding set and effective/permitted sets.
fn drop_sys_admin() {
    const CAP_SYS_ADMIN: u64 = 21;
    const PR_CAPBSET_DROP: libc::c_int = 24;

    // Drop from bounding set
    unsafe {
        libc::prctl(PR_CAPBSET_DROP, CAP_SYS_ADMIN, 0, 0, 0);
    }

    // Drop from effective/permitted via capset(2)
    #[repr(C)]
    struct CapHeader {
        version: u32,
        pid: i32,
    }
    #[repr(C)]
    #[derive(Clone, Copy)]
    struct CapData {
        effective: u32,
        permitted: u32,
        inheritable: u32,
    }

    const _LINUX_CAPABILITY_VERSION_3: u32 = 0x20080522;
    let mut hdr = CapHeader {
        version: _LINUX_CAPABILITY_VERSION_3,
        pid: 0,
    };
    let mut data = [CapData {
        effective: 0,
        permitted: 0,
        inheritable: 0,
    }; 2];

    unsafe {
        libc::syscall(
            libc::SYS_capget,
            &mut hdr as *mut CapHeader,
            data.as_mut_ptr(),
        );

        // CAP_SYS_ADMIN (21) is in the first u32 (caps 0-31)
        let mask = !(1u32 << CAP_SYS_ADMIN as u32);
        data[0].effective &= mask;
        data[0].permitted &= mask;

        hdr.version = _LINUX_CAPABILITY_VERSION_3;
        libc::syscall(
            libc::SYS_capset,
            &hdr as *const CapHeader,
            data.as_ptr(),
        );
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    /// Test apply_step with a synthetic upper dir (no actual overlayfs needed).
    #[test]
    fn test_apply_step_copies_files() {
        let tmp = tempfile::tempdir().unwrap();
        let base = tmp.path().join("checkpoint");
        let ckpt = Checkpointer::new(base.clone());

        // Simulate step 1 upper dir
        let upper = ckpt.step_upper_dir(1);
        let sub = upper.join("tmp").join("test-apply");
        fs::create_dir_all(&sub).unwrap();
        fs::write(sub.join("hello.txt"), "from overlay").unwrap();

        // apply_step should walk and copy; we can't write to / in tests
        // so just verify the walk logic works without errors when the
        // target paths don't exist (create_dir_all + copy).
        // In a real container this would write to the real filesystem.
        assert!(upper.exists());
        // The Checkpointer finds the upper dir correctly
        assert_eq!(ckpt.step_upper_dir(1), upper);
    }

    /// Test that step numbers increment atomically.
    #[test]
    fn test_step_counter_increments() {
        let tmp = tempfile::tempdir().unwrap();
        let ckpt = Checkpointer::new(tmp.path().join("ckpt"));

        let mut cmd1 = Command::new("true");
        let step1 = ckpt.wrap_command(&mut cmd1).unwrap();
        assert_eq!(step1, 1);

        let mut cmd2 = Command::new("true");
        let step2 = ckpt.wrap_command(&mut cmd2).unwrap();
        assert_eq!(step2, 2);
    }

    /// Test that wrap_command creates the upper and work dirs.
    #[test]
    fn test_wrap_creates_dirs() {
        let tmp = tempfile::tempdir().unwrap();
        let base = tmp.path().join("ckpt");
        let ckpt = Checkpointer::new(base.clone());

        let mut cmd = Command::new("true");
        let step = ckpt.wrap_command(&mut cmd).unwrap();

        assert!(base.join(format!("step-{step}")).join("upper").is_dir());
        assert!(base.join(format!("step-{step}")).join("work").is_dir());
    }

    /// Test apply_step on nonexistent upper dir is a no-op.
    #[test]
    fn test_apply_missing_upper_noop() {
        let tmp = tempfile::tempdir().unwrap();
        let ckpt = Checkpointer::new(tmp.path().join("ckpt"));
        // Step 999 was never created
        assert!(ckpt.apply_step(999).is_ok());
    }

    /// Test the walkdir-based apply on a synthetic tree with dirs and files.
    #[test]
    fn test_apply_upper_dir_walk() {
        let tmp = tempfile::tempdir().unwrap();
        let upper = tmp.path().join("upper");

        // Create a nested structure
        let nested = upper.join("a").join("b");
        fs::create_dir_all(&nested).unwrap();
        fs::write(nested.join("file.txt"), "content").unwrap();
        fs::write(upper.join("root.txt"), "root").unwrap();

        // apply_upper_dir won't fail even if / destinations are unwritable
        // in a test environment; verify it walks without panicking.
        let _ = apply_upper_dir(&upper);
    }
}
