//! Connection authentication for the executor protocol.
//!
//! The executor's remote transports (TCP, QUIC) are an arbitrary-code-execution
//! surface: `SpawnRequest` runs commands, `Read`/`Write` touch the filesystem.
//! They used to rely purely on network isolation, so any peer that could route
//! to the port owned the sandbox.
//!
//! The token is claimed on first use rather than configured. A warm sandbox
//! runs no user code — the container command is this agent, not the image
//! entrypoint — so the gateway claims the token while the pod is still empty.
//! Afterwards the claim is fixed for the life of the process, and a caller from
//! another sandbox cannot authenticate without that pod's token.
//!
//! The Unix socket is deliberately exempt: its peers already live inside the
//! container and can read anything the agent could protect.

use std::sync::Mutex;

/// The token this executor answers to, claimed by the first `Auth` request.
static CLAIMED: Mutex<Option<String>> = Mutex::new(None);

/// Outcome of an `Auth` request.
pub enum AuthOutcome {
    /// This call established the token.
    Claimed,
    /// The token matched the existing claim.
    Matched,
    /// The token did not match; the connection stays unauthenticated.
    Rejected,
}

/// Whether remote transports must authenticate. Disabled only for local
/// development via `EXECUTOR_AUTH_REQUIRED=0`.
pub fn auth_required() -> bool {
    !matches!(
        std::env::var("EXECUTOR_AUTH_REQUIRED").as_deref(),
        Ok("0") | Ok("false")
    )
}

/// Compare without leaking where two tokens diverge.
fn constant_time_eq(a: &str, b: &str) -> bool {
    let (a, b) = (a.as_bytes(), b.as_bytes());
    if a.len() != b.len() {
        return false;
    }
    let mut diff = 0u8;
    for (x, y) in a.iter().zip(b.iter()) {
        diff |= x ^ y;
    }
    diff == 0
}

/// Claim the token, or check a presented one against an existing claim.
pub fn authenticate(token: &str) -> AuthOutcome {
    if token.is_empty() {
        return AuthOutcome::Rejected;
    }
    let mut claimed = CLAIMED.lock().unwrap();
    match claimed.as_deref() {
        None => {
            *claimed = Some(token.to_string());
            AuthOutcome::Claimed
        }
        Some(existing) if constant_time_eq(existing, token) => AuthOutcome::Matched,
        Some(_) => AuthOutcome::Rejected,
    }
}

#[cfg(test)]
pub fn reset_for_test() {
    *CLAIMED.lock().unwrap() = None;
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn first_token_is_claimed_then_enforced() {
        reset_for_test();

        assert!(matches!(authenticate("secret-a"), AuthOutcome::Claimed));
        assert!(matches!(authenticate("secret-a"), AuthOutcome::Matched));
        // A different sandbox's token must not open this one.
        assert!(matches!(authenticate("secret-b"), AuthOutcome::Rejected));
        // Rejection does not overwrite the claim.
        assert!(matches!(authenticate("secret-a"), AuthOutcome::Matched));
    }

    #[test]
    fn empty_token_never_authenticates() {
        reset_for_test();

        assert!(matches!(authenticate(""), AuthOutcome::Rejected));
        // ...and must not have claimed the empty string either.
        assert!(matches!(authenticate("real"), AuthOutcome::Claimed));
    }

    #[test]
    fn constant_time_eq_matches_semantics() {
        assert!(constant_time_eq("abc", "abc"));
        assert!(!constant_time_eq("abc", "abd"));
        assert!(!constant_time_eq("abc", "abcd"));
        assert!(constant_time_eq("", ""));
    }

    #[test]
    fn auth_is_required_unless_explicitly_disabled() {
        unsafe { std::env::remove_var("EXECUTOR_AUTH_REQUIRED") };
        assert!(auth_required(), "must fail closed by default");

        unsafe { std::env::set_var("EXECUTOR_AUTH_REQUIRED", "0") };
        assert!(!auth_required());

        unsafe { std::env::set_var("EXECUTOR_AUTH_REQUIRED", "1") };
        assert!(auth_required());
        unsafe { std::env::remove_var("EXECUTOR_AUTH_REQUIRED") };
    }
}
