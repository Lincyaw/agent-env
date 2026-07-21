use std::collections::HashMap;
use std::fs;
use std::io;
use std::os::unix::fs::{MetadataExt, PermissionsExt};
use std::path::{Path, PathBuf};
use std::sync::atomic::{AtomicU32, Ordering};

/// Per-file metadata used for pre/post diff comparison.
#[derive(Debug, Clone, PartialEq, Eq)]
struct FileMeta {
    mtime_ns: u64,
    size: u64,
    ino: u64,
}

/// Opaque snapshot of the filesystem state at a point in time.
pub struct FsSnapshot {
    files: HashMap<PathBuf, FileMeta>,
}

/// Captures per-step filesystem diffs using pre/post stat comparison.
///
/// Before each command, `pre_scan()` walks the filesystem and records
/// (mtime_ns, size, inode) for every regular file. After the command
/// completes, `capture_diff()` re-scans and copies changed/new files
/// into the step upper directory, creating whiteout markers for deleted
/// files. The command runs directly on the real filesystem -- no overlay,
/// no chroot, no mount namespace.
pub struct Checkpointer {
    base_dir: PathBuf,
    scan_root: PathBuf,
    step: AtomicU32,
}

impl Checkpointer {
    pub fn new(base_dir: PathBuf) -> Self {
        let _ = fs::create_dir_all(&base_dir);
        Self {
            base_dir,
            scan_root: PathBuf::from("/"),
            step: AtomicU32::new(0),
        }
    }

    /// Allocate the next step number.
    /// Used by file write operations to record uploads in the checkpoint.
    pub fn next_step(&self) -> u32 {
        self.step.fetch_add(1, Ordering::Relaxed) + 1
    }

    /// Returns the upper dir path for a given step.
    pub fn step_upper_dir(&self, step: u32) -> PathBuf {
        self.base_dir.join(format!("step-{step}")).join("upper")
    }

    /// Scan the filesystem and store state for later diffing.
    pub fn pre_scan(&self) -> io::Result<FsSnapshot> {
        let files = scan_filesystem(&self.scan_root, &self.base_dir)?;
        log::info!("[checkpoint] pre_scan captured {} files", files.len());
        Ok(FsSnapshot { files })
    }

    /// Compare current filesystem against the snapshot, copy changed files
    /// to step-N/upper, create whiteout markers for deleted files.
    /// Returns the list of changed paths (for logging).
    pub fn capture_diff(&self, step: u32, snapshot: &FsSnapshot) -> io::Result<Vec<String>> {
        let after = scan_filesystem(&self.scan_root, &self.base_dir)?;
        let upper = self.step_upper_dir(step);
        fs::create_dir_all(&upper)?;

        let mut changed = Vec::new();

        // New and modified files
        for (path, after_meta) in &after {
            match snapshot.files.get(path) {
                Some(before_meta) if before_meta == after_meta => continue,
                _ => {
                    let rel = path.strip_prefix("/").unwrap_or(path);
                    let dst = upper.join(rel);
                    if let Some(parent) = dst.parent() {
                        fs::create_dir_all(parent)?;
                    }
                    if let Err(e) = copy_entry(path, &dst) {
                        log::warn!("[checkpoint] copy {} failed: {e}", path.display());
                        continue;
                    }
                    changed.push(path.display().to_string());
                }
            }
        }

        // Deleted files: create overlayfs-compatible whiteout markers
        for path in snapshot.files.keys() {
            if !after.contains_key(path) {
                let rel = path.strip_prefix("/").unwrap_or(path);
                let dst = upper.join(rel);
                if let Some(parent) = dst.parent() {
                    fs::create_dir_all(parent)?;
                }
                // char device with major=0, minor=0
                unsafe {
                    let c_path = std::ffi::CString::new(
                        dst.to_str().unwrap_or_default(),
                    )
                    .unwrap();
                    libc::mknod(c_path.as_ptr(), libc::S_IFCHR | 0o666, libc::makedev(0, 0));
                }
                changed.push(format!("(deleted) {}", path.display()));
            }
        }

        make_world_readable(&upper);
        log::info!("[checkpoint] step={step} captured {} changes", changed.len());
        Ok(changed)
    }

    /// Create a combined tar of steps 1..=through, writing to `out`.
    pub fn write_combined_tar<W: io::Write>(&self, through: u32, out: &mut W) -> io::Result<()> {
        let mut builder = tar::Builder::new(out);
        for step in 1..=through {
            let upper = self.step_upper_dir(step);
            if !upper.exists() {
                continue;
            }
            for entry in walkdir::WalkDir::new(&upper).follow_links(false) {
                let entry = match entry {
                    Ok(e) => e,
                    Err(_) => continue,
                };
                let path = entry.path();
                if path == upper {
                    continue;
                }
                let rel = path.strip_prefix(&upper).unwrap();
                let meta = match fs::symlink_metadata(path) {
                    Ok(m) => m,
                    Err(_) => continue,
                };
                if meta.is_file() {
                    if let Err(e) = builder.append_path_with_name(path, rel) {
                        log::warn!("[checkpoint] tar append file {:?}: {e}", rel);
                    }
                } else if meta.file_type().is_symlink() {
                    let mut header = tar::Header::new_gnu();
                    header.set_entry_type(tar::EntryType::Symlink);
                    let target = match fs::read_link(path) {
                        Ok(t) => t,
                        Err(_) => continue,
                    };
                    header.set_size(0);
                    if let Err(e) = builder.append_link(&mut header, rel, target) {
                        log::warn!("[checkpoint] tar append symlink {:?}: {e}", rel);
                    }
                }
            }
        }
        builder.finish()?;
        Ok(())
    }

    /// Create a tar of a single step's upper dir, writing to `out`.
    pub fn write_single_step_tar<W: io::Write>(&self, step: u32, out: &mut W) -> io::Result<()> {
        let mut builder = tar::Builder::new(out);
        let upper = self.step_upper_dir(step);
        if !upper.exists() {
            builder.finish()?;
            return Ok(());
        }
        for entry in walkdir::WalkDir::new(&upper).follow_links(false) {
            let entry = match entry {
                Ok(e) => e,
                Err(_) => continue,
            };
            let path = entry.path();
            if path == upper {
                continue;
            }
            let rel = path.strip_prefix(&upper).unwrap();
            let meta = match fs::symlink_metadata(path) {
                Ok(m) => m,
                Err(_) => continue,
            };
            if meta.is_file() {
                if let Err(e) = builder.append_path_with_name(path, rel) {
                    log::warn!("[checkpoint] tar append file {:?}: {e}", rel);
                }
            } else if meta.file_type().is_symlink() {
                let mut header = tar::Header::new_gnu();
                header.set_entry_type(tar::EntryType::Symlink);
                let target = match fs::read_link(path) {
                    Ok(t) => t,
                    Err(_) => continue,
                };
                header.set_size(0);
                if let Err(e) = builder.append_link(&mut header, rel, target) {
                    log::warn!("[checkpoint] tar append symlink {:?}: {e}", rel);
                }
            }
        }
        builder.finish()?;
        Ok(())
    }

    /// List available checkpoint step numbers.
    pub fn list_steps(&self) -> Vec<u32> {
        let mut steps = Vec::new();
        if let Ok(entries) = fs::read_dir(&self.base_dir) {
            for entry in entries.flatten() {
                let name = entry.file_name();
                if let Some(n) = name.to_str().and_then(|s| s.strip_prefix("step-")) {
                    if let Ok(step) = n.parse::<u32>() {
                        steps.push(step);
                    }
                }
            }
        }
        steps.sort();
        steps
    }

    #[cfg(test)]
    fn with_scan_root(mut self, root: PathBuf) -> Self {
        self.scan_root = root;
        self
    }
}

/// Copy a file or symlink to the upper dir, preserving symlink targets.
fn copy_entry(src: &Path, dst: &Path) -> io::Result<()> {
    let meta = fs::symlink_metadata(src)?;
    if meta.file_type().is_symlink() {
        let target = fs::read_link(src)?;
        // Remove existing entry at dst if any, then create symlink
        let _ = fs::remove_file(dst);
        std::os::unix::fs::symlink(&target, dst)?;
    } else {
        fs::copy(src, dst)?;
    }
    Ok(())
}

/// Walk the filesystem from `root` and collect metadata for every regular file
/// and symlink. Symlinks are tracked by their own lstat metadata (not the
/// target), so a changed or new symlink is captured correctly.
fn scan_filesystem(root: &Path, base_dir: &Path) -> io::Result<HashMap<PathBuf, FileMeta>> {
    let mut state = HashMap::new();
    for entry in walkdir::WalkDir::new(root)
        .follow_links(false)
        .into_iter()
        .filter_entry(|e| {
            let p = e.path();
            !p.starts_with("/proc")
                && !p.starts_with("/sys")
                && !p.starts_with("/dev")
                && !p.starts_with(base_dir)
        })
    {
        let entry = match entry {
            Ok(e) => e,
            Err(_) => continue,
        };
        let ft = entry.file_type();
        if !ft.is_file() && !ft.is_symlink() {
            continue;
        }
        // Use lstat (symlink's own metadata, not target)
        let meta = match fs::symlink_metadata(entry.path()) {
            Ok(m) => m,
            Err(_) => continue,
        };
        state.insert(
            entry.path().to_path_buf(),
            FileMeta {
                mtime_ns: meta.mtime() as u64 * 1_000_000_000
                    + meta.mtime_nsec() as u64,
                size: meta.len(),
                ino: meta.ino(),
            },
        );
    }
    Ok(state)
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

#[cfg(test)]
mod tests {
    use super::*;

    fn test_checkpointer(tmp: &Path) -> Checkpointer {
        let base = tmp.join("ckpt");
        Checkpointer::new(base).with_scan_root(tmp.to_path_buf())
    }

    #[test]
    fn test_step_counter_increments() {
        let tmp = tempfile::tempdir().unwrap();
        let ckpt = test_checkpointer(tmp.path());

        let s1 = ckpt.next_step();
        assert_eq!(s1, 1);
        let s2 = ckpt.next_step();
        assert_eq!(s2, 2);
    }

    #[test]
    fn test_step_upper_dir() {
        let tmp = tempfile::tempdir().unwrap();
        let base = tmp.path().join("ckpt");
        let ckpt = Checkpointer::new(base.clone()).with_scan_root(tmp.path().to_path_buf());

        let upper = ckpt.step_upper_dir(3);
        assert_eq!(upper, base.join("step-3").join("upper"));
    }

    #[test]
    fn test_pre_scan_captures_files() {
        let tmp = tempfile::tempdir().unwrap();
        let target = tmp.path().join("existing.txt");
        fs::write(&target, "data").unwrap();

        let ckpt = test_checkpointer(tmp.path());
        let snap = ckpt.pre_scan().unwrap();

        assert!(
            snap.files.contains_key(&target),
            "pre_scan should capture the test file"
        );
    }

    #[test]
    fn test_capture_diff_detects_new_file() {
        let tmp = tempfile::tempdir().unwrap();
        let ckpt = test_checkpointer(tmp.path());

        let snap = ckpt.pre_scan().unwrap();

        let new_file = tmp.path().join("new_file.txt");
        fs::write(&new_file, "hello").unwrap();

        let step = ckpt.next_step();
        let changed = ckpt.capture_diff(step, &snap).unwrap();

        assert!(
            changed.iter().any(|p| p.contains("new_file.txt")),
            "new file should be in changed list: {:?}",
            changed
        );

        let rel = new_file.strip_prefix("/").unwrap();
        let upper_file = ckpt.step_upper_dir(step).join(rel);
        assert!(upper_file.exists(), "new file should be copied to upper dir");
        assert_eq!(fs::read_to_string(&upper_file).unwrap(), "hello");
    }

    #[test]
    fn test_capture_diff_detects_modified_file() {
        let tmp = tempfile::tempdir().unwrap();
        let target = tmp.path().join("mod_file.txt");
        fs::write(&target, "before").unwrap();

        let ckpt = test_checkpointer(tmp.path());
        let snap = ckpt.pre_scan().unwrap();

        fs::write(&target, "after-modified").unwrap();

        let step = ckpt.next_step();
        let changed = ckpt.capture_diff(step, &snap).unwrap();

        assert!(
            changed.iter().any(|p| p.contains("mod_file.txt")),
            "modified file should be in changed list: {:?}",
            changed
        );

        let rel = target.strip_prefix("/").unwrap();
        let upper_file = ckpt.step_upper_dir(step).join(rel);
        assert_eq!(fs::read_to_string(&upper_file).unwrap(), "after-modified");
    }

    #[test]
    fn test_capture_diff_detects_deleted_file() {
        let tmp = tempfile::tempdir().unwrap();
        let target = tmp.path().join("del_file.txt");
        fs::write(&target, "to-delete").unwrap();

        let ckpt = test_checkpointer(tmp.path());
        let snap = ckpt.pre_scan().unwrap();

        fs::remove_file(&target).unwrap();

        let step = ckpt.next_step();
        let changed = ckpt.capture_diff(step, &snap).unwrap();

        assert!(
            changed.iter().any(|p| p.contains("(deleted)") && p.contains("del_file.txt")),
            "deleted file should appear with (deleted) prefix: {:?}",
            changed
        );
    }

    #[test]
    fn test_capture_diff_skips_unchanged() {
        let tmp = tempfile::tempdir().unwrap();
        let target = tmp.path().join("stable.txt");
        fs::write(&target, "stable").unwrap();

        let ckpt = test_checkpointer(tmp.path());
        let snap = ckpt.pre_scan().unwrap();

        let step = ckpt.next_step();
        let changed = ckpt.capture_diff(step, &snap).unwrap();

        assert!(
            !changed.iter().any(|p| p.contains("stable.txt")),
            "unchanged file should NOT be in changed list: {:?}",
            changed
        );
    }
}
