import { memo } from "react";
import ReactMarkdown from "react-markdown";
import remarkGfm from "remark-gfm";

/** Rendered markdown (GFM: tables, task lists, strikethrough). Raw HTML is not rendered. */
export const MarkdownPreview = memo(function MarkdownPreview({ source }: { source: string }) {
  return (
    <div className="md-preview">
      <ReactMarkdown
        remarkPlugins={[remarkGfm]}
        components={{
          a: ({ href, children }) => (
            <a href={href} target={href?.startsWith("/") ? undefined : "_blank"} rel="noopener noreferrer">
              {children}
            </a>
          ),
          img: ({ src, alt }) => <img src={typeof src === "string" ? src : undefined} alt={alt ?? ""} loading="lazy" />,
        }}
      >
        {source || "*Nothing here yet.*"}
      </ReactMarkdown>
    </div>
  );
});
