fn fixture() -> u32 { 1 }

#[test]
fn connects() {
    assert_eq!(fixture(), 1);
    let _ = golden::connect("x");
}
