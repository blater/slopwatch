pub fn distinct_one(value: i32) -> i32 {
    if value > 0 {
        if value % 2 == 0 { return value * 2; }
    }
    value
}
