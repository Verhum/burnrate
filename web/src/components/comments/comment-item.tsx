import Markdown from "react-markdown";
import remarkGfm from "remark-gfm";
import type { Comment } from "@/lib/api/types";
import { baseUrl } from "@/lib/api/client";
import { resolveAttachmentSrc } from "@/lib/composer-attachments";
import { formatRelativeTime } from "@/lib/format";
import { parseRunOutput, SECTION_LABELS } from "@/lib/parse-output";
import type { RunOutput } from "@/lib/parse-output";

// eslint-disable-next-line @typescript-eslint/no-explicit-any -- react-markdown injects extra props beyond HTMLAttributes
const markdownComponents: Record<string, React.ComponentType<any>> = {
  img: (rawProps: { node?: unknown } & React.ImgHTMLAttributes<HTMLImageElement>) => {
    const { node: _node, src: rawSrc, ...props } = rawProps;
    void _node;
    const src = typeof rawSrc === "string" ? resolveAttachmentSrc(rawSrc, baseUrl) : rawSrc;
    // eslint-disable-next-line @next/next/no-img-element, jsx-a11y/alt-text -- alt comes from react-markdown via ...props
    return <img alt="" {...props} src={src} crossOrigin="anonymous" />;
  },
};

interface CommentItemProps {
  comment: Comment;
}

function OutputSection({
  field,
  body,
}: {
  field: keyof Omit<RunOutput, "raw">;
  body: string;
}) {
  if (!body) return null;
  return (
    <div className="mb-2">
      <div className="text-[9px] font-bold uppercase tracking-widest text-muted mb-0.5">
        {SECTION_LABELS[field]}
      </div>
      <div className="text-xs text-primary prose-comment">
        <Markdown remarkPlugins={[remarkGfm]} components={markdownComponents}>{body}</Markdown>
      </div>
    </div>
  );
}

function StructuredOutput({ body }: { body: string }) {
  const out = parseRunOutput(body);
  const hasStructure =
    out.summary || out.changes || out.verify || out.docs || out.bootstrap;

  if (!hasStructure) {
    return (
      <div className="text-xs text-primary prose-comment">
        <Markdown remarkPlugins={[remarkGfm]} components={markdownComponents}>{body}</Markdown>
      </div>
    );
  }

  return (
    <div className="flex flex-col gap-0">
      <OutputSection field="summary" body={out.summary} />
      <OutputSection field="changes" body={out.changes} />
      <OutputSection field="verify" body={out.verify} />
      <OutputSection field="docs" body={out.docs} />
      <OutputSection field="bootstrap" body={out.bootstrap} />
    </div>
  );
}

export function CommentItem({ comment }: CommentItemProps) {
  const isAgent = comment.author === "agent";

  return (
    <div
      className={`px-4 py-3 ${
        isAgent ? "bg-elevated ml-4" : "bg-surface mr-4"
      }`}
    >
      <div className="flex items-center gap-2 mb-1.5">
        <span
          className={`text-[9px] font-bold uppercase tracking-widest px-1.5 py-0.5 ${
            isAgent ? "bg-raised text-amber" : "bg-raised text-dim"
          }`}
        >
          {comment.author}
        </span>
        <span className="text-[10px] text-muted">
          {formatRelativeTime(comment.created_at)}
        </span>
      </div>
      {isAgent ? (
        <StructuredOutput body={comment.body} />
      ) : (
        <div className="text-xs text-primary prose-comment">
          <Markdown remarkPlugins={[remarkGfm]} components={markdownComponents}>{comment.body}</Markdown>
        </div>
      )}
    </div>
  );
}
