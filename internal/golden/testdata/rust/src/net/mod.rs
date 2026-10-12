pub mod client;

pub use self::client::Client;

pub trait Transport {
    fn send(&self, data: &[u8]) -> usize;
    fn flush(&self) {}
}

impl Transport for Client {
    fn send(&self, data: &[u8]) -> usize {
        Client::send(self, data)
    }
}
