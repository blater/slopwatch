pub fn rescan(values: &[i32], queries: &[i32]) -> i32 {
    let mut found = 0;
    for query in queries {
        for (i, value) in values.iter().enumerate() {
            if value == query { found += i as i32; break; }
        }
    }
    found
}
