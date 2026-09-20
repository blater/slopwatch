pub fn unused_before(x: i32) -> i32 {
    let used = x * 2;
    if x > 0 {
        let unused = x * 3;
        let _ = unused;
    }
    used + 1
}
