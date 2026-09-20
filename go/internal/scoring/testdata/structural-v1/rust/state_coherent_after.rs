pub struct CoherentAfter { total: i32 }

impl CoherentAfter {
    fn advance_state(&mut self, delta: i32) -> i32 {
        if delta < 0 { return self.total; }
        self.total += delta;
        if self.total > 100 { self.total = 100; }
        self.total
    }

    pub fn advance(&mut self, delta: i32) -> i32 { self.advance_state(delta) }
    pub fn advance_again(&mut self, delta: i32) -> i32 { self.advance_state(delta) }
}
