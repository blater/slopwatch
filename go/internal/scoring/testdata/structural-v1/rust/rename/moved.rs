pub fn relocated_routine(value: i32) -> i32 {
    if value < 0 { return -value; }
    value * 2
}
