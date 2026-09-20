pub struct CosmeticAfter { total: i32, updates: i32 }

impl CosmeticAfter {
    fn accept_state(delta: i32) -> bool {
        if delta < 0 { return false; }
        true
    }
    fn set_state(&mut self, delta: i32) {
        self.total += delta;
        if self.total > 100 { self.total = 100; }
    }
    fn mark_state(&mut self) { self.updates += 1; }

    pub fn advance(&mut self, delta: i32) -> i32 {
        if !Self::accept_state(delta) { return self.total; }
        self.set_state(delta);
        self.mark_state();
        self.total
    }
}
