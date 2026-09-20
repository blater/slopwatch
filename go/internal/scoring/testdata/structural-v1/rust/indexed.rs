use std::collections::HashMap;

pub fn indexed(values: &[i32], queries: &[i32]) -> i32 {
    let mut index = HashMap::new();
    for (i, value) in values.iter().enumerate() { index.insert(*value, i as i32); }
    let mut found = 0;
    for query in queries { found += index.get(query).copied().unwrap_or(0); }
    found
}
