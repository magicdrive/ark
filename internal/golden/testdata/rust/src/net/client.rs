use super::Transport;

pub struct Config {
    pub addr: String,
}

impl Config {
    pub fn default_for(addr: &str) -> Self {
        Config { addr: addr.to_string() }
    }
}

pub struct Client {
    config: Config,
}

impl Client {
    pub fn new(config: Config) -> Self {
        Self { config }
    }

    pub fn send(&self, data: &[u8]) -> usize {
        data.len()
    }

    pub fn describe(&self, t: &str) -> String {
        format!("{} via {}", self.config.addr, t)
    }
}

pub fn flush_all<T: Transport>(items: &[T]) {
    for item in items {
        item.flush();
    }
}
