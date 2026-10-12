mod memory;

pub use self::memory::Memory;
use super::model::User;

pub struct Repository {
    backend: Memory,
}

impl Repository {
    pub fn new() -> Self {
        Repository { backend: Memory::default() }
    }

    pub fn save(&mut self, user: User) {
        self.backend.put(user);
    }
}

pub trait Store {
    fn save(&mut self, user: User);
}

impl Store for Repository {
    fn save(&mut self, user: User) {
        Repository::save(self, user)
    }
}
