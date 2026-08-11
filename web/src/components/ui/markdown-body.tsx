import Markdown from "react-markdown";
import remarkGfm from "remark-gfm";

interface MarkdownBodyProps {
  children: string;
  /** Extra classes for the wrapper — colour and size live with the caller. */
  className?: string;
}

/**
 * Agent- and human-authored markdown, rendered the same way everywhere: GFM
 * on, raw HTML off (react-markdown's default), `.prose-comment` for spacing.
 * The comment thread established this pairing; request bodies and demo briefs
 * reuse it so a `**bold**` never reaches the screen as literal asterisks.
 */
export function MarkdownBody({ children, className }: MarkdownBodyProps) {
  return (
    <div className={`prose-comment${className ? ` ${className}` : ""}`}>
      <Markdown remarkPlugins={[remarkGfm]}>{children}</Markdown>
    </div>
  );
}
