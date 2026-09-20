pub fn guards_before(a: bool, b: bool) -> bool {
    if a {
        if b { return true; }
    }
    false
}
