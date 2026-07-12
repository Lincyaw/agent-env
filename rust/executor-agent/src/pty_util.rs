use nix::pty::{openpty, OpenptyResult};
use std::io;
use std::os::fd::{AsRawFd, OwnedFd};

pub struct PtyPair {
    pub master: OwnedFd,
    pub slave: OwnedFd,
}

/// Open a new PTY pair and set the initial window size.
pub fn open_pty(rows: u16, cols: u16) -> io::Result<PtyPair> {
    let OpenptyResult { master, slave } = openpty(None, None)
        .map_err(|e| io::Error::other(format!("openpty: {}", e)))?;

    // Set window size on the slave fd
    set_winsize(master.as_raw_fd(), rows, cols)?;

    Ok(PtyPair { master, slave })
}

/// Set the terminal window size via ioctl TIOCSWINSZ.
pub fn set_winsize(fd: i32, rows: u16, cols: u16) -> io::Result<()> {
    let ws = libc::winsize {
        ws_row: rows,
        ws_col: cols,
        ws_xpixel: 0,
        ws_ypixel: 0,
    };
    let ret = unsafe { libc::ioctl(fd, libc::TIOCSWINSZ, &ws) };
    if ret < 0 {
        Err(io::Error::last_os_error())
    } else {
        Ok(())
    }
}

/// Prepare the slave side for use as a controlling terminal in a child process.
/// This must be called after fork(), in the child, before exec().
/// It creates a new session, sets the controlling terminal, and dups
/// the slave fd to stdin/stdout/stderr.
pub fn setup_slave_terminal(slave_fd: i32) -> io::Result<()> {
    // Create a new session
    nix::unistd::setsid()
        .map_err(|e| io::Error::other(format!("setsid: {}", e)))?;

    // Set controlling terminal
    let ret = unsafe { libc::ioctl(slave_fd, libc::TIOCSCTTY as libc::c_ulong, 0i32) };
    if ret < 0 {
        return Err(io::Error::last_os_error());
    }

    // Dup slave fd to stdin/stdout/stderr
    nix::unistd::dup2(slave_fd, 0)
        .map_err(|e| io::Error::other(format!("dup2 stdin: {}", e)))?;
    nix::unistd::dup2(slave_fd, 1)
        .map_err(|e| io::Error::other(format!("dup2 stdout: {}", e)))?;
    nix::unistd::dup2(slave_fd, 2)
        .map_err(|e| io::Error::other(format!("dup2 stderr: {}", e)))?;

    // Close original slave fd if it's not one of 0, 1, 2
    if slave_fd > 2 {
        nix::unistd::close(slave_fd)
            .map_err(|e| io::Error::other(format!("close slave: {}", e)))?;
    }

    Ok(())
}

