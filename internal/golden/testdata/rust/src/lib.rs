pub mod net;
mod broken;

use net::client::{Client, Config as Cfg};

#[macro_export]
macro_rules! check {
    ($e:expr) => {
        assert!($e)
    };
}

pub const RETRIES: u32 = 3;

pub fn connect(addr: &str) -> Client {
    let cfg = Cfg::default_for(addr);
    check!(RETRIES > 0);
    Client::new(cfg)
}

pub fn shadowed(connect: fn(&str)) {
    connect("x");
}

#[cfg(feature = "tls")]
pub fn transport() -> &'static str { "tls" }
#[cfg(not(feature = "tls"))]
pub fn transport() -> &'static str { "tcp" }

pub fn describe(c: &Client) -> String {
    let t = transport();
    c.describe(t)
}
