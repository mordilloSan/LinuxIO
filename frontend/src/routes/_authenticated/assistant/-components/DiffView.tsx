import { diffLines } from "diff";

export default function DiffView({
  path,
  oldText,
  newText,
}: {
  path: string;
  oldText?: string | null;
  newText: string;
}) {
  const rows = diffLines(oldText ?? "", newText).flatMap((part, partIndex) => {
    const kind = part.added ? "add" : part.removed ? "del" : "ctx";
    const sign = part.added ? "+" : part.removed ? "-" : " ";
    return part.value
      .replace(/\n$/, "")
      .split("\n")
      .map((line, lineIndex) => (
        <div
          className={`assistant-diff__${kind}`}
          key={`${partIndex}:${lineIndex}`}
        >
          {sign}
          {line}
        </div>
      ));
  });
  return (
    <div className="assistant-diff">
      <div className="assistant-diff__path">{path}</div>
      <pre>{rows}</pre>
    </div>
  );
}
