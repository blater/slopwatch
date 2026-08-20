// Macro bodies are intentionally not expanded by safe analysis.
macro_rules! generated_branch { () => { if true { 1 } else { 2 } }; }

pub struct Repository<T, E: std::error::Error> {
    value: Result<Vec<T>, E>,
}

pub fn nested<T>(value: Option<Result<T, String>>) -> usize {
    if let Some(result) = value {
        while result.is_ok() {
            unsafe { return 1; }
        }
    }
    todo!("fixture")
}
