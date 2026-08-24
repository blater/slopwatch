import ts from "typescript";
import type { Measurement, SourceEntry, Subject } from "./model.js";
import {
  fieldUsageByMethod,
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
    const usage = methods.map((method) => fieldUsageByMethod(method));
    const fields = usage.map((item) => item.own);
    const methodsByField = new Map<string, number[]>();
    for (let method = 0; method < fields.length; method++) {
      for (const field of fields[method] ?? []) {
        const owners = methodsByField.get(field);
        if (owners === undefined) methodsByField.set(field, [method]);
        else owners.push(method);
      }
    }
    let connected = 0;
    for (let left = 0; left < fields.length; left++) {
      const connectedRights = new Set<number>();
      for (const field of fields[left] ?? []) {
        for (const right of methodsByField.get(field) ?? []) {
          if (right > left) connectedRights.add(right);
        }
      }
      connected += connectedRights.size;
    }
    const pairs = (fields.length * (fields.length - 1)) / 2;
    const tcc = pairs === 0 ? 0 : connected / pairs;
    const wmc =
      typeMemberCount(statement) +
      methods.reduce((sum, method) => sum + cyclomatic(method), 0);
    const foreignTypes = uniqueStrings(typeReferences(statement));
    const foreignFields = uniqueStrings(
      usage.flatMap((item) => [...item.foreign]),
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
