use std::fmt;

pub struct User {
    pub id: u64,
    name: String,
}

pub enum Role {
    Admin,
    Member(u32),
}

impl User {
    pub fn new(id: u64) -> Self {
        Self { id, name: String::new() }
    }

    pub fn role(&self) -> Role {
        if self.id == 0 { Role::Admin } else { Role::Member(1) }
    }
}

impl fmt::Display for User {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        write!(f, "{}", self.name)
    }
}

pub trait Named {
    fn name(&self) -> String;
    fn greet(&self) -> String {
        Self::name(self)
    }
}

impl Named for User {
    fn name(&self) -> String {
        self.name.clone()
    }
}

/// Through the crate root's re-export (`pub use storage::Repository as Repo`).
pub fn repository() -> crate::Repo {
    crate::Repo::new()
}
