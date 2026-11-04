type Snapshot = { content: string; saved: string; saving: boolean; error: Error | null; ready: boolean };

// The editor survives route changes so an in-flight write cannot discard a newer draft.
export class PreferenceAutosave {
  private snapshot: Snapshot = { content: '', saved: '', saving: false, error: null, ready: false };
  private listeners = new Set<() => void>();
  private timer: ReturnType<typeof setTimeout> | undefined;
  private running: Promise<void> | undefined;
  private composing = false;
  constructor(private write: (content: string) => Promise<unknown>) {}
  getSnapshot = () => this.snapshot;
  subscribe = (listener: () => void) => { this.listeners.add(listener); return () => { this.listeners.delete(listener); }; };
  private update(patch: Partial<Snapshot>) {
    this.snapshot = { ...this.snapshot, ...patch };
    this.listeners.forEach(listener => listener());
  }
  load(content: string) {
    if (this.snapshot.ready && (this.snapshot.saving || this.snapshot.content !== this.snapshot.saved)) return;
    this.update({ content, saved: content, ready: true });
  }
  edit(content: string) {
    this.update({ content, error: null });
    this.schedule();
  }
  setComposing(composing: boolean) {
    this.composing = composing;
    clearTimeout(this.timer);
    if (!composing) this.schedule();
  }
  private schedule() {
    clearTimeout(this.timer);
    if (!this.composing) this.timer = setTimeout(() => { void this.flush(); }, 600);
  }
  flush = (): Promise<void> => {
    clearTimeout(this.timer);
    if (this.running) return this.running;
    if (this.composing || !this.snapshot.ready || this.snapshot.content === this.snapshot.saved) return Promise.resolve();
    this.running = this.drain().finally(() => { this.running = undefined; });
    return this.running;
  };
  private async drain() {
    this.update({ saving: true, error: null });
    try {
      while (!this.composing && this.snapshot.content !== this.snapshot.saved) {
        const content = this.snapshot.content;
        await this.write(content);
        this.update({ saved: content });
      }
    } catch (error) {
      this.update({ error: error instanceof Error ? error : new Error(String(error)) });
    } finally { this.update({ saving: false }); }
  }
  dispose() { clearTimeout(this.timer); }
}
