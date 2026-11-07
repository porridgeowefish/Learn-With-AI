import { useCallback, useEffect, useRef, useState } from 'react';
import { http } from '@/shared/client';
import { encodeWave } from './chatAudio';

// Only the current PCM segment and unacknowledged requests retain audio.
// Successful segments are discarded; there is no total recording timer limit.
class VoiceTranscription {
  private samples: Float32Array[] = [];
  private sampleCount = 0;
  private queue: ArrayBuffer[] = [];
  private text: string[] = [];
  private worker: Promise<void> | null = null;
  private failure: Error | null = null;
  readonly controller = new AbortController();
  constructor(private slug: string, private rate: number, private onFailure: (error: Error) => void) {}

  append(data: Float32Array) {
    const capacity = Math.floor(this.rate * 30);
    for (let offset = 0; offset < data.length;) {
      const count = Math.min(capacity - this.sampleCount, data.length - offset);
      this.samples.push(new Float32Array(data.subarray(offset, offset + count)));
      this.sampleCount += count; offset += count;
      if (this.sampleCount === capacity) this.flush();
    }
  }
  flush() {
    if (!this.sampleCount) return;
    const samples = new Float32Array(this.sampleCount);
    let offset = 0;
    for (const chunk of this.samples) { samples.set(chunk, offset); offset += chunk.length; }
    this.samples = []; this.sampleCount = 0;
    this.queue.push(encodeWave(samples, this.rate));
    void this.drain();
  }
  private drain(): Promise<void> {
    if (this.worker) return this.worker;
    if (this.failure || this.controller.signal.aborted) return Promise.resolve();
    this.worker = (async () => {
      try {
        while (this.queue.length && !this.controller.signal.aborted) {
          const response = await fetch(`/api/projects/${encodeURIComponent(this.slug)}/conversation/transcribe`, {
            method: 'POST', headers: { 'Content-Type': 'audio/wav' }, body: this.queue[0], signal: this.controller.signal,
          });
          const result = await response.json();
          if (!response.ok) throw new Error(typeof result.error === 'string' ? result.error : '语音转写失败');
          if (typeof result.text !== 'string') throw new Error('语音转写结果无效，请重试');
          if (this.controller.signal.aborted) return;
          this.queue.shift();
          if (result.text.trim()) this.text.push(result.text.trim());
        }
      } catch (error) {
        if (!this.controller.signal.aborted) { this.failure = error as Error; this.onFailure(this.failure); }
      } finally { this.worker = null; }
    })();
    return this.worker;
  }
  async finish(): Promise<string> {
    this.flush();
    if (this.worker) await this.worker;
    if (this.failure) throw this.failure;
    await this.drain();
    if (this.failure) throw this.failure;
    return this.text.join('\n');
  }
  retry() { this.failure = null; }
  cancel() { this.controller.abort(); this.samples = []; this.queue = []; this.text = []; }
}

interface Capture {
  context: AudioContext; stream: MediaStream; source: MediaStreamAudioSourceNode;
  processor: ScriptProcessorNode; mute: GainNode; timer: number;
}

export function useVoiceInput(slug: string) {
  const [recording, setRecording] = useState(false);
  const [pending, setPending] = useState(false);
  const [hasRecording, setHasRecording] = useState(false);
  const [seconds, setSeconds] = useState(0);
  const [error, setError] = useState('');
  const capture = useRef<Capture | null>(null);
  const draft = useRef<VoiceTranscription | null>(null);
  const generation = useRef(0);
  const requesting = useRef<AbortController | null>(null);
  const finishing = useRef<Promise<string | undefined> | null>(null);
  const releaseCapture = useCallback(() => {
    const current = capture.current; capture.current = null;
    if (!current) return;
    clearInterval(current.timer); current.processor.onaudioprocess = null;
    current.processor.disconnect(); current.source.disconnect(); current.mute.disconnect();
    current.stream.getTracks().forEach(track => track.stop()); void current.context.close();
  }, []);
  const clear = useCallback(() => {
    generation.current++; requesting.current?.abort(); requesting.current = null;
    releaseCapture(); draft.current?.cancel(); draft.current = null; finishing.current = null;
  }, [releaseCapture]);
  useEffect(() => {
    setRecording(false); setPending(false); setHasRecording(false); setSeconds(0); setError('');
    return clear;
  }, [slug, clear]);
  const cancelRecording = () => {
    clear(); setRecording(false); setPending(false); setHasRecording(false); setError(''); setSeconds(0);
  };

  const stopRecording = (): Promise<string | undefined> => {
    if (finishing.current) return finishing.current;
    const current = draft.current;
    if (!current) return Promise.resolve(undefined);
    releaseCapture(); setRecording(false); setPending(true); setError('');
    current.retry();
    const token = generation.current;
    const result = (async () => {
      try {
        const text = await current.finish();
        if (token !== generation.current || draft.current !== current) return undefined;
        current.cancel(); draft.current = null; setHasRecording(false);
        if (!text.trim()) { setError('没有识别到语音，请重新录音。'); return undefined; }
        return text;
      } catch (cause) {
        if (token === generation.current) setError(`转写未完成：${(cause as Error).message}。可重试转写或取消。`);
        return undefined;
      } finally {
        if (token === generation.current) { finishing.current = null; setPending(false); }
      }
    })();
    finishing.current = result;
    return result;
  };

  const startRecording = async () => {
    if (capture.current || draft.current || requesting.current || finishing.current) return;
    if (!window.confirm('是否允许使用系统麦克风？录音将在本机转写。停止后可编辑文字，也可以直接转写并发送。')) return;
    if (!navigator.mediaDevices?.getUserMedia) { setError('当前环境无法访问麦克风，请检查系统和浏览器权限'); return; }
    const token = generation.current;
    const controller = new AbortController(); requesting.current = controller;
    setPending(true); setError('');
    let stream: MediaStream | null = null, context: AudioContext | null = null;
    try {
      const capability = await http.get<{ languages: string[]; setupHint: string }>('/api/system/speech', { signal: controller.signal });
      if (token !== generation.current) return;
      if (!capability.languages?.length) throw new Error(capability.setupHint || '系统未安装中文或英文的本地语音识别');
      stream = await navigator.mediaDevices.getUserMedia({ audio: { channelCount: 1, echoCancellation: true }, video: false });
      if (token !== generation.current) { stream.getTracks().forEach(track => track.stop()); return; }
      context = new AudioContext(); await context.resume();
      if (token !== generation.current) { stream.getTracks().forEach(track => track.stop()); await context.close(); return; }
      const source = context.createMediaStreamSource(stream), processor = context.createScriptProcessor(4096, 1, 1), mute = context.createGain();
      mute.gain.value = 0;
      const current = new VoiceTranscription(slug, context.sampleRate, cause => {
        if (token !== generation.current) return;
        releaseCapture(); setRecording(false); current.flush();
        setError(`转写暂未完成，录音已停止：${cause.message}。已识别内容保留，可重试转写。`);
      });
      draft.current = current;
      processor.onaudioprocess = event => current.append(event.inputBuffer.getChannelData(0));
      source.connect(processor); processor.connect(mute); mute.connect(context.destination);
      const started = Date.now(); setSeconds(0);
      const timer = window.setInterval(() => setSeconds(Math.floor((Date.now() - started) / 1000)), 1000);
      capture.current = { stream, context, source, processor, mute, timer };
      stream.getTracks().forEach(track => track.addEventListener('ended', () => {
        if (token !== generation.current || !capture.current) return;
        releaseCapture(); current.flush(); setRecording(false); setError('麦克风已断开，可停止转写以取回文字。');
      }, { once: true }));
      setHasRecording(true); setRecording(true);
    } catch (cause) {
      stream?.getTracks().forEach(track => track.stop()); if (context && context.state !== 'closed') void context.close();
      if (token === generation.current) {
        releaseCapture(); draft.current?.cancel(); draft.current = null; setHasRecording(false); setRecording(false);
        setError((cause as Error).name === 'NotAllowedError' ? '麦克风权限未获允许，请在系统或浏览器设置中授权后重试' : (cause as Error).message);
      }
    } finally {
      if (token === generation.current) { requesting.current = null; setPending(false); }
    }
  };

  return { recording, pending, hasRecording, seconds, error, startRecording, stopRecording, cancelRecording };
}
