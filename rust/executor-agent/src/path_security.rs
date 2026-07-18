use std::path::{Path, PathBuf};

/// Validate and clean a file path. Rejects NUL bytes, empty paths, and
/// relative paths (all paths must be absolute). The container itself is
/// the security boundary.
pub fn sanitize_path(user_path: &str) -> Result<PathBuf, String> {
    if user_path.contains('\0') {
        return Err("path must not contain NUL bytes".to_string());
    }

    let clean = clean_path(user_path);

    if clean == "." || clean.is_empty() {
        return Err("path is required".to_string());
    }

    if !Path::new(&clean).is_absolute() {
        return Err("path must be absolute".to_string());
    }

    let target_path = PathBuf::from(&clean);

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

    #[test]
    fn test_reject_nul_bytes() {
        let result = sanitize_path("foo\0bar");
        assert!(result.is_err());
        assert!(result.unwrap_err().contains("NUL"));
    }

    #[test]
    fn test_reject_empty_path() {
        let result = sanitize_path(".");
        assert!(result.is_err());
        assert!(result.unwrap_err().contains("required"));
    }

    #[test]
    fn test_accept_absolute_path() {
        let result = sanitize_path("/tmp/test.txt");
        assert!(result.is_ok());
        assert_eq!(result.unwrap(), PathBuf::from("/tmp/test.txt"));
    }

    #[test]
    fn test_reject_relative_path() {
        let result = sanitize_path("subdir/file.txt");
        assert!(result.is_err());
        assert!(result.unwrap_err().contains("absolute"));
    }

    #[test]
    fn test_reject_relative_with_dotdot() {
        let result = sanitize_path("subdir/../file.txt");
        assert!(result.is_err());
        assert!(result.unwrap_err().contains("absolute"));
    }
}
