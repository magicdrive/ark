//! The app library.
pub mod storage;
pub mod model;

pub use storage::Repository as Repo;
use model::User;

#[macro_export]
macro_rules! log_line {
    ($e:expr) => {
        let _ = $e;
    };
}

pub fn open() -> storage::Repository {
    let repo = storage::Repository::new();
    log_line!(1);
    repo
}

pub fn first_user() -> User {
    User::new(1)
}

#[cfg(unix)]
pub fn platform() -> &'static str { "unix" }
#[cfg(not(unix))]
pub fn platform() -> &'static str { "other" }

pub fn which() -> &'static str {
    platform()
}
