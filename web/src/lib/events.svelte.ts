import { SvelteSet } from "svelte/reactivity";

// Live updates over Server-Sent Events. The browser resumes dropped connections by itself and sends
// Last-Event-ID; when the server cannot replay (journal pruned, database replaced) it says so and
// subscribers reload their data.
export interface ChangeEvent {
  seq: number;
  type: string;
  project: string;
  key?: string;
}

type Listener = (event: ChangeEvent) => void;

const listeners = new SvelteSet<Listener>();
const resyncListeners = new SvelteSet<() => void>();

export const realtime = $state({ connected: false });

let source: EventSource | undefined;
let lastSeq = 0;
let generation = "";
let retry: ReturnType<typeof setTimeout> | undefined;

export function onChange(listener: Listener): () => void {
  listeners.add(listener);
  return () => listeners.delete(listener);
}

export function onResync(listener: () => void): () => void {
  resyncListeners.add(listener);
  return () => resyncListeners.delete(listener);
}

function resync(): void {
  resyncListeners.forEach((l) => {
    l();
  });
}

export function connect(): void {
  if (source) return;
  const es = new EventSource(
    lastSeq > 0 ? `/api/v1/events?since=${String(lastSeq)}` : "/api/v1/events",
  );
  source = es;

  es.addEventListener("hello", (m) => {
    const hello = JSON.parse((m as MessageEvent<string>).data) as {
      generation: string;
      seq: number;
    };
    realtime.connected = true;
    if (generation && generation !== hello.generation) resync();
    generation = hello.generation;
    if (lastSeq === 0) lastSeq = hello.seq;
  });
  es.addEventListener("resync_required", (m) => {
    lastSeq = (JSON.parse((m as MessageEvent<string>).data) as { seq: number })
      .seq;
    resync();
  });
  es.onmessage = (m: MessageEvent<string>) => {
    const event = JSON.parse(m.data) as ChangeEvent;
    lastSeq = event.seq;
    listeners.forEach((l) => {
      l(event);
    });
  };
  es.onerror = () => {
    realtime.connected = false;
    // CONNECTING means the browser is retrying on its own; CLOSED (for example after a 401) needs a new connection.
    if (es.readyState === EventSource.CLOSED) {
      source = undefined;
      clearTimeout(retry);
      retry = setTimeout(connect, 5000);
    }
  };
}

export function disconnect(): void {
  clearTimeout(retry);
  source?.close();
  source = undefined;
  realtime.connected = false;
  lastSeq = 0;
  generation = "";
}
