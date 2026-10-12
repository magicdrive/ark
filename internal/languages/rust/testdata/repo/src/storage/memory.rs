use crate::model::User;

#[derive(Default)]
pub struct Memory {
    users: Vec<User>,
}

impl Memory {
    pub fn put(&mut self, user: User) {
        self.users.push(user);
    }
}
