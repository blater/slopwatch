pub fn dispatch_flat(kind: i32, value: i32) -> i32 {
    match kind {
        0 => value + 1,
        1 => value * 2,
        _ => value - 1,
    }
}
