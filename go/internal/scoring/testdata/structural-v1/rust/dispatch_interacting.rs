pub fn dispatch_interacting(kind: i32, value: i32) -> i32 {
    let mut state = 0;
    if kind == 0 {
        state += value;
        if value < 0 { state = 0; }
    } else if kind == 1 {
        state += value * 2;
        if state > 10 { state = 10; }
    } else {
        state -= value;
    }
    state
}
