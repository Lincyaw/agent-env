use std::path::{Path, PathBuf};

/// Resolve a relative path within the workspace, preventing directory traversal
/// attacks. Mirrors the Go resolveWorkspacePath function exactly.
pub fn resolve_workspace_path(workspace_dir: &Path, rel_path: &str) -> Result<PathBuf, String> {
    // Reject NUL bytes
    if rel_path.contains('\0') {
        return Err("path must not contain NUL bytes".to_string());
    }

    // Clean the path (normalize . and ..)
    let clean = clean_path(rel_path);

    if clean == "." || clean.is_empty() {
        return Err("path is required".to_string());
    }

    // Reject absolute paths
    if Path::new(&clean).is_absolute() {
        return Err("path must be relative to the workspace".to_string());
    }

    // Resolve workspace root to absolute, following symlinks
    let workspace_root = match workspace_dir.canonicalize() {
        Ok(p) => p,
        Err(_) => match std::path::absolute(workspace_dir) {
            Ok(p) => p,
            Err(e) => return Err(format!("resolve workspace: {}", e)),
        },
    };

    let target_path = workspace_root.join(&clean);

    // Resolve symlinks on target (or longest existing prefix)
    let resolved_target = if let Ok(resolved) = target_path.canonicalize() {
        resolved
    } else {
        // Target doesn't fully exist yet; resolve parent
        let dir = target_path.parent().unwrap_or(&target_path);
        if let Ok(resolved_dir) = dir.canonicalize() {
            resolved_dir.join(target_path.file_name().unwrap_or_default())
        } else {
            target_path.clone()
        }
    };

    // Check that resolved target is within workspace root
    match resolved_target.strip_prefix(&workspace_root) {
        Ok(rel) => {
            let rel_str = rel.to_string_lossy();
            if rel_str == ".." || rel_str.starts_with("../") {
                return Err("path must stay within the workspace".to_string());
            }
            Ok(resolved_target)
        }
        Err(_) => Err("path must stay within the workspace".to_string()),
    }
}

/// Simplified path cleaning that normalizes `.` and `..` components,
/// matching Go's filepath.Clean behavior.
fn clean_path(p: &str) -> String {
    let path = Path::new(p);
    let mut components = Vec::new();

    for component in path.components() {
        match component {
            std::path::Component::CurDir => {}
            std::path::Component::ParentDir => {
                if components.last().is_some_and(|c: &String| c != "..") {
                    components.pop();
                } else {
                    components.push("..".to_string());
                }
            }
            std::path::Component::Normal(s) => {
                components.push(s.to_string_lossy().to_string());
            }
            std::path::Component::RootDir => {
                components.push("/".to_string());
            }
            std::path::Component::Prefix(p) => {
                components.push(p.as_os_str().to_string_lossy().to_string());
            }
        }
    }

    if components.is_empty() {
        ".".to_string()
    } else {
        components.join("/")
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use std::fs;

    #[test]
    fn test_reject_nul_bytes() {
        let dir = tempfile::tempdir().unwrap();
        let result = resolve_workspace_path(dir.path(), "foo\0bar");
        assert!(result.is_err());
        assert!(result.unwrap_err().contains("NUL"));
    }

    #[test]
    fn test_reject_empty_path() {
        let dir = tempfile::tempdir().unwrap();
        let result = resolve_workspace_path(dir.path(), ".");
        assert!(result.is_err());
        assert!(result.unwrap_err().contains("required"));
    }

    #[test]
    fn test_reject_absolute_path() {
        let dir = tempfile::tempdir().unwrap();
        let result = resolve_workspace_path(dir.path(), "/etc/passwd");
        assert!(result.is_err());
        assert!(result.unwrap_err().contains("relative"));
    }

    #[test]
    fn test_reject_parent_traversal() {
        let dir = tempfile::tempdir().unwrap();
        let result = resolve_workspace_path(dir.path(), "../../../etc/passwd");
        assert!(result.is_err());
        assert!(result.unwrap_err().contains("within the workspace"));
    }

    #[test]
    fn test_valid_relative_path() {
        let dir = tempfile::tempdir().unwrap();
        fs::create_dir_all(dir.path().join("subdir")).unwrap();
        let result = resolve_workspace_path(dir.path(), "subdir/file.txt");
        assert!(result.is_ok());
    }
}
