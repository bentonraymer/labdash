import { Link } from "react-router";
import { ListTodo } from "lucide-react";
import { usePlanningSummary } from "@/lib/queries";
import { todosLink } from "@/lib/planning";

/** Open-todo count for a service or host, linking to those todos. */
export function TodoBadge({ service, host }: { service?: number; host?: number }) {
  const { data } = usePlanningSummary();
  const n = service ? data?.openByService[String(service)] : host ? data?.openByHost[String(host)] : 0;
  if (!n) return null;
  return (
    <Link
      to={todosLink({ service, host })}
      onClick={(e) => e.stopPropagation()}
      title={`${n} open todo${n === 1 ? "" : "s"}`}
      className="inline-flex items-center gap-0.5 rounded bg-accent/15 px-1 py-px text-[10px] font-medium text-accent hover:bg-accent/25"
    >
      <ListTodo size={10} /> {n}
    </Link>
  );
}
