use serde_json::{json, Value};
use std::io::{self, Write};

pub fn write(frame: &Value) -> Result<(), String> {
    let mut output = io::stdout().lock();
    serde_json::to_writer(&mut output, frame).map_err(|error| error.to_string())?;
    output.write_all(b"\n").and_then(|_| output.flush()).map_err(|error| error.to_string())
}

pub fn progress(enabled: bool, stage: &str, completed: usize, total: usize) -> Result<(), String> {
    if enabled { write(&json!({"type": "progress", "stage": stage, "completed": completed, "total": total}))?; }
    Ok(())
}
