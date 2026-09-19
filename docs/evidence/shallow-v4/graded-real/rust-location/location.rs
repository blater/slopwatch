use crate::model::Location;
use proc_macro2::Span;

pub fn location(path: &str, span: Span) -> Location {
    let start = span.start();
    let end = span.end();
    Location {
        path: path.to_owned(),
        line: start.line,
        column: start.column + 1,
        end_line: end.line,
        end_column: end.column + 1,
    }
}
