import ts from "typescript";
import type { Measurement, SourceEntry, Subject } from "./model.js";
import {
  fieldsUsedByMethod,
  foreignFieldsUsedByMethod,
  isTypeDeclaration,
  methodLike,
  typeMemberCount,
  typeMembers,
  typeReferences,
  uniqueStrings,
  type FunctionFact,
} from "./surface.js";

function subject(source: ts.SourceFile, node: ts.Node, name: string): Subject {
  const start = source.getLineAndCharacterOfPosition(node.getStart(source));
  const end = source.getLineAndCharacterOfPosition(node.getEnd());
  return {
    name,
    symbol: name,
    start: {
      line: start.line + 1,
      column: start.character + 1,
      offset: node.getStart(source),
    },
    end: {
      line: end.line + 1,
      column: end.character + 1,
      offset: node.getEnd(),
    },
  };
}

function measurement(
  entry: SourceEntry,
  component: string,
  definition: string,
  scope: "function" | "file" | "type" | "expression",
  value: number,
  item: Subject,
  attributes: Record<string, unknown>,
): Measurement {
  return {
    unit_id: entry.unitId,
    component_id: component,
    definition_version: definition,
    path: entry.relativePath,
    scope,
    value,
    subject: item,
    attributes,
    provenance: {
      analyzer: "slopslap-typescript",
      analyzer_version: "0.1.0",
      rule: `${component}/${definition}`,
    },
  };
}

export function typeMetricMeasurements(
  entry: SourceEntry,
  requestedComponents: ReadonlySet<string>,
  cyclomatic: (fact: FunctionFact) => number,
): Measurement[] {
  const output: Measurement[] = [];
  for (const statement of entry.sourceFile.statements) {
    if (!isTypeDeclaration(statement)) continue;
    const methods = typeMembers(statement, entry.sourceFile);
    const fields = methods.map((method) => fieldsUsedByMethod(method));
    let connected = 0;
    let pairs = 0;
    for (let left = 0; left < fields.length; left++)
      for (let right = left + 1; right < fields.length; right++) {
        pairs++;
        const leftFields = fields[left] ?? new Set<string>();
        const rightFields = fields[right] ?? new Set<string>();
        if ([...leftFields].some((field) => rightFields.has(field)))
          connected++;
      }
    const tcc = pairs === 0 ? 0 : connected / pairs;
    const wmc =
      typeMemberCount(statement) +
      methods.reduce((sum, method) => sum + cyclomatic(method), 0);
    const foreignTypes = uniqueStrings(typeReferences(statement));
    const foreignFields = uniqueStrings(
      methods.flatMap((method) => [...foreignFieldsUsedByMethod(method)]),
    );
    const item = subject(
      entry.sourceFile,
      statement,
      statement.name?.text ?? "<anonymous-type>",
    );
    const kind = ts.isInterfaceDeclaration(statement) ? "interface" : "class";
    if (requestedComponents.has("cyclomatic_class_complexity"))
      output.push(
        measurement(
          entry,
          "cyclomatic_class_complexity",
          "pmd-v1",
          "type",
          wmc,
          item,
          { kind },
        ),
      );
    if (requestedComponents.has("coupling_between_objects"))
      output.push(
        measurement(
          entry,
          "coupling_between_objects",
          "pmd-v1",
          "type",
          foreignTypes.length,
          item,
          { kind, referenced_types: foreignTypes },
        ),
      );
    if (
      requestedComponents.has("god_class") &&
      ts.isClassDeclaration(statement)
    )
      output.push(
        measurement(
          entry,
          "god_class",
          "pmd-v1",
          "type",
          wmc,
          subject(
            entry.sourceFile,
            statement,
            statement.name?.text ?? "<anonymous-class>",
          ),
          { kind: "class", wmc, atfd: foreignFields.length, tcc },
        ),
      );
  }
  return output;
}
