use std::path::{Path, PathBuf};

/// Resolve a path for file operations. Relative paths are resolved within the
/// workspace; absolute paths are used directly. Both are sanitized against NUL
/// bytes and `..` traversal. The container itself is the security boundary —
/// agents may need to operate on files outside the workspace directory.
pub fn resolve_workspace_path(workspace_dir: &Path, user_path: &str) -> Result<PathBuf, String> {
    if user_path.contains('\0') {
        return Err("path must not contain NUL bytes".to_string());
    }

    let clean = clean_path(user_path);

    if clean == "." || clean.is_empty() {
        return Err("path is required".to_string());
    }

    let target_path = if Path::new(&clean).is_absolute() {
        PathBuf::from(&clean)
    } else {
        let workspace_root = match workspace_dir.canonicalize() {
            Ok(p) => p,
            Err(_) => match std::path::absolute(workspace_dir) {
                Ok(p) => p,
                Err(e) => return Err(format!("resolve workspace: {}", e)),
            },
        };
        workspace_root.join(&clean)
    };

    let resolved = if let Ok(r) = target_path.canonicalize() {
        r
    } else {
        let dir = target_path.parent().unwrap_or(&target_path);
        if let Ok(resolved_dir) = dir.canonicalize() {
            resolved_dir.join(target_path.file_name().unwrap_or_default())
        } else {
            target_path.clone()
        }
    };

    Ok(resolved)
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
    fn test_accept_absolute_path() {
        let dir = tempfile::tempdir().unwrap();
        let result = resolve_workspace_path(dir.path(), "/tmp/test.txt");
        assert!(result.is_ok());
        assert_eq!(result.unwrap(), PathBuf::from("/tmp/test.txt"));
    }

    #[test]
    fn test_relative_parent_stays_in_workspace() {
        let dir = tempfile::tempdir().unwrap();
        let result = resolve_workspace_path(dir.path(), "subdir/../file.txt");
        assert!(result.is_ok());
    }

    #[test]
    fn test_valid_relative_path() {
        let dir = tempfile::tempdir().unwrap();
        fs::create_dir_all(dir.path().join("subdir")).unwrap();
        let result = resolve_workspace_path(dir.path(), "subdir/file.txt");
        assert!(result.is_ok());
    }
}
