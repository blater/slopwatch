mod control;
mod design;
mod expression;
mod location;
mod model;
mod parser;
mod surface;

use model::{Request, Response, SCHEMA_VERSION};
use std::io::{self, Read};
use std::path::Path;

fn response(request: Request) -> Response {
    if request.schema_version != SCHEMA_VERSION || request.source_paths.is_empty() {
        return Response {
            schema_version: SCHEMA_VERSION,
            program: None,
            error: Some("unsupported or empty structural fact request".to_owned()),
        };
    }
    match parser::parse_program(
        Path::new(&request.workspace),
        &request.source_paths,
        request.include_tests,
    ) {
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
    let mut payload = String::new();
    if io::stdin()
        .take(16 * 1024 * 1024)
        .read_to_string(&mut payload)
        .is_err()
    {
        std::process::exit(2);
    }
    let result = match serde_json::from_str::<Request>(&payload) {
        Ok(request) => response(request),
        Err(error) => Response {
            schema_version: SCHEMA_VERSION,
            program: None,
            error: Some(format!("invalid structural fact request: {error}")),
        },
    };
    if serde_json::to_writer(io::stdout(), &result).is_err() {
        std::process::exit(2);
    }
}
