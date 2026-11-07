import { useEffect, useMemo, useState, type ReactNode } from "react";
import { useNavigate } from "react-router";
import { Background, Controls, Handle, MiniMap, Position, ReactFlow, type Edge, type Node, type NodeProps } from "@xyflow/react";
import "@xyflow/react/dist/style.css";
import ELK from "elkjs/lib/elk.bundled.js";
import clsx from "clsx";
import { useHosts, useLaunchpad } from "@/lib/queries";
import { hostKindIcon, hostKindLabel, runtimeLabel } from "@/lib/labels";
import type { Host, Service, Status } from "@/lib/types";
import { ServiceIcon } from "@/components/ServiceIcon";
import { StatusDot } from "@/components/StatusDot";
import { PveStats, hostResource, usePve } from "@/components/IntegrationBits";
import { useUI } from "@/components/UIProvider";
import { PageHeader } from "./AdminLayout";

const elk = new ELK();
const W = { host: 200, service: 180 };
const H = { host: 64, service: 48 };

type HostData = { host: Host; usage?: ReactNode; itself?: Service[] };
type ServiceData = { service: Service };

function HostNode({ data }: NodeProps<Node<HostData>>) {
  const h = data.host;
  const Icon = hostKindIcon[h.kind];
  const top = !h.parentId;
  return (
    <div className={clsx("w-[200px] rounded-lg border px-3 py-2 shadow-lg", top ? "border-accent/50 bg-[#141a2a]" : "border-line-2 bg-panel-2")}>
      <Handle type="target" position={Position.Top} className="!border-0 !bg-line-2" />
      <div className="flex items-center gap-2">
        <Icon size={15} className={top ? "text-accent" : "text-muted"} />
        <span className="truncate text-[13px] font-semibold">{h.name}</span>
        {h.vmid != null && <span className="ml-auto font-mono text-[10px] text-faint">#{h.vmid}</span>}
      </div>
      <div className="mt-0.5 flex items-center gap-2 text-[10px] text-muted">
        <span>{hostKindLabel[h.kind]}</span>
        {h.ip && <span className="font-mono">{h.ip}</span>}
      </div>
      {data.itself?.map((s) => (
        <div key={s.id} className="mt-1.5 flex items-center gap-1.5 rounded bg-white/5 px-1.5 py-1 text-[11px]">
          <ServiceIcon icon={s.icon} name={s.name} className="size-4" />
          <span className="flex-1 truncate">{s.name}</span>
          <StatusDot status={s.status} className="size-2" />
        </div>
      ))}
      {data.usage && <div className="mt-1.5">{data.usage}</div>}
      <Handle type="source" position={Position.Bottom} className="!border-0 !bg-line-2" />
    </div>
  );
}

const ring: Record<Status, string> = {
  up: "border-up/40",
  down: "border-down/70 shadow-[0_0_14px_-2px_rgba(255,77,109,0.6)]",
  pending: "border-warn/50",
  maintenance: "border-maint/50",
  paused: "border-line-2",
  unknown: "border-line-2",
};

function ServiceNode({ data }: NodeProps<Node<ServiceData>>) {
  const s = data.service;
  return (
    <div className={clsx("glass flex w-[180px] items-center gap-2 rounded-lg border px-2.5 py-2", ring[s.status])}>
      <Handle type="target" position={Position.Top} className="!border-0 !bg-line-2" />
      <ServiceIcon icon={s.icon} name={s.name} className="size-6 shrink-0" />
      <div className="min-w-0 flex-1">
        <div className="truncate text-[12px] font-medium">{s.name}</div>
        <div className="truncate text-[10px] text-faint">
          {s.hosting ? runtimeLabel[s.hosting.runtime] : ""}
          {s.hosting?.vmid ? ` #${s.hosting.vmid}` : s.hosting?.containerName ? ` · ${s.hosting.containerName}` : ""}
        </div>
      </div>
      <StatusDot status={s.status} />
    </div>
  );
}

const nodeTypes = { host: HostNode, service: ServiceNode };

/** Hosts → guests → services, laid out top-down with ELK. */
export function TopologyPage() {
  const { data: hosts = [] } = useHosts();
  const { data: lp } = useLaunchpad();
  const pve = usePve();
  const ui = useUI();
  const navigate = useNavigate();
  const [nodes, setNodes] = useState<Node[]>([]);
  const [edges, setEdges] = useState<Edge[]>([]);

  const services = useMemo(() => lp?.services ?? [], [lp?.services]);
  const placed = services.filter((s) => s.hosting?.hostId && hosts.some((h) => h.id === s.hosting!.hostId));
  // Bare-metal services are the host itself: drawn inside the host's card.
  const hosted = placed.filter((s) => s.hosting!.runtime !== "bare_metal");
  const itselfOf = (hostId: number) => placed.filter((s) => s.hosting!.runtime === "bare_metal" && s.hosting!.hostId === hostId);
  // Re-layout only when structure changes, not on every status/stat refresh.
  const shapeKey = JSON.stringify([hosts.map((h) => [h.id, h.parentId, itselfOf(h.id).length]), hosted.map((s) => [s.id, s.hosting?.hostId])]);

  useEffect(() => {
    if (!hosts.length) {
      setNodes([]);
      setEdges([]);
      return;
    }
    const ids = new Set(hosts.map((h) => h.id));
    const graph = {
      id: "root",
      layoutOptions: {
        "elk.algorithm": "layered",
        "elk.direction": "DOWN",
        "elk.layered.spacing.nodeNodeBetweenLayers": "70",
        "elk.spacing.nodeNode": "28",
        "elk.layered.nodePlacement.strategy": "BRANDES_KOEPF",
        // All top-level hosts start in the first row.
        "elk.layered.layering.strategy": "LONGEST_PATH_SOURCE",
        // Lay out all trees together instead of packing each into rows.
        "elk.separateConnectedComponents": "false",
      },
      children: [
        ...hosts.map((h) => ({ id: `h${h.id}`, width: W.host, height: H.host + (pve ? 22 : 0) + itselfOf(h.id).length * 30 })),
        ...hosted.map((s) => ({ id: `s${s.id}`, width: W.service, height: H.service })),
      ],
      edges: [
        ...hosts.filter((h) => h.parentId && ids.has(h.parentId)).map((h) => ({ id: `e-h${h.id}`, sources: [`h${h.parentId}`], targets: [`h${h.id}`] })),
        ...hosted.map((s) => ({ id: `e-s${s.id}`, sources: [`h${s.hosting!.hostId}`], targets: [`s${s.id}`] })),
      ],
    };
    let cancelled = false;
    elk.layout(graph).then((res) => {
      if (cancelled) return;
      const pos = new Map((res.children ?? []).map((c) => [c.id, { x: c.x ?? 0, y: c.y ?? 0 }]));
      setNodes([
        ...hosts.map<Node>((h) => ({ id: `h${h.id}`, type: "host", position: pos.get(`h${h.id}`)!, data: { host: h } })),
        ...hosted.map<Node>((s) => ({ id: `s${s.id}`, type: "service", position: pos.get(`s${s.id}`)!, data: { service: s } })),
      ]);
      setEdges(
        graph.edges.map((e) => ({
          id: e.id,
          source: e.sources[0],
          target: e.targets[0],
          type: "smoothstep",
          style: { stroke: "var(--color-line-2)", strokeWidth: 1.5 },
        })),
      );
    });
    return () => {
      cancelled = true;
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [shapeKey, !!pve]);

  // Refresh node data (status, stats) without moving anything.
  const liveNodes = useMemo(
    () =>
      nodes.map((n) => {
        if (n.type === "host") {
          const h = hosts.find((x) => `h${x.id}` === n.id);
          if (!h) return n;
          const r = hostResource(pve, h);
          return { ...n, data: { host: h, itself: itselfOf(h.id), usage: r ? <PveStats r={r} compact /> : undefined } };
        }
        const s = services.find((x) => `s${x.id}` === n.id);
        return s ? { ...n, data: { service: s } } : n;
      }),
    [nodes, hosts, services, pve],
  );

  const unhosted = services.length - placed.length;
  return (
    <div className="flex h-full flex-col">
      <PageHeader
        title="Topology"
        subtitle={`${hosts.length} hosts · ${placed.length} services placed${unhosted ? ` · ${unhosted} without a host` : ""} · click a node to edit it`}
      />
      <div className="relative min-h-0 flex-1">
        {hosts.length === 0 ? (
          <div className="p-10 text-center text-sm text-muted">Add hosts (Proxmox nodes, VMs, LXCs) and link services to them to see the map.</div>
        ) : (
          <ReactFlow
            nodes={liveNodes}
            edges={edges}
            nodeTypes={nodeTypes}
            fitView
            fitViewOptions={{ padding: 0.15 }}
            minZoom={0.2}
            nodesDraggable
            nodesConnectable={false}
            proOptions={{ hideAttribution: true }}
            colorMode="dark"
            onNodeClick={(_, n) => {
              if (n.type === "service") ui.editService((n.data as ServiceData).service);
              else navigate("/hosts");
            }}
          >
            <Background color="#1f2533" gap={22} />
            <Controls showInteractive={false} className="!border-line-2 [&_button]:!border-line-2 [&_button]:!bg-panel-2 [&_button]:!fill-muted" />
            <MiniMap pannable zoomable maskColor="rgba(10,12,17,0.75)" className="!bg-panel" nodeColor={(n) => (n.type === "host" ? "#5b8cff" : "#2a3142")} />
          </ReactFlow>
        )}
      </div>
    </div>
  );
}
