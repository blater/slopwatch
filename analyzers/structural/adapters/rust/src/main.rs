mod control;
mod depth;
mod design;
mod expression;
mod location;
mod model;
mod parser;
mod surface;
mod stream;

use model::{Request, Response, SCHEMA_VERSION};
use std::io::{self, Read};
use std::path::Path;

fn read_payload(mut input: impl Read) -> io::Result<String> {
    let mut payload = String::new();
    input.read_to_string(&mut payload)?;
    Ok(payload)
}

fn response(request: Request, stream: bool) -> Response {
    if request.schema_version != SCHEMA_VERSION || request.source_paths.is_empty() {
        return Response {
            schema_version: SCHEMA_VERSION,
            program: None,
            error: Some("unsupported or empty structural fact request".to_owned()),
        };
    }
    let program = if stream {
        parser::parse_program_progress(Path::new(&request.workspace), &request.source_paths, request.include_tests, request.depth, true)
    } else {
        parser::parse_program(Path::new(&request.workspace), &request.source_paths, request.include_tests, request.depth)
    };
    match program {
        Ok(program) => Response {
            schema_version: SCHEMA_VERSION,
            program: Some(program),
            error: None,
        },
        Err(error) => Response {
            schema_version: SCHEMA_VERSION,
            program: None,
            error: Some(error),
        },
    }
}

fn main() {
    let payload = read_payload(io::stdin()).unwrap_or_else(|_| std::process::exit(2));
    let stream = std::env::args().any(|arg| arg == "--stream");
    let result = match serde_json::from_str::<Request>(&payload) {
        Ok(request) => response(request, stream),
        Err(error) => Response {
            schema_version: SCHEMA_VERSION,
            program: None,
            error: Some(format!("invalid structural fact request: {error}")),
        },
    };
    if stream {
        let terminal = serde_json::json!({"type": "done", "schema_version": SCHEMA_VERSION, "error": result.error});
        if stream::write(&terminal).is_err() { std::process::exit(2); }
        return;
    }
    if serde_json::to_writer(io::stdout(), &result).is_err() {
        std::process::exit(2);
    }
}

#[cfg(test)]
mod tests {
    use super::read_payload;
    use std::io::Cursor;

    #[test]
    fn reads_requests_larger_than_the_previous_limit() {
        let payload = vec![b' '; 17 * 1024 * 1024];
        let read = read_payload(Cursor::new(&payload)).expect("large request is readable");
        assert_eq!(read.len(), payload.len());
    }
}
