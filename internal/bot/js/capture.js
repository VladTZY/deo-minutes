// Captures the meeting's remote audio inside the page: every remote WebRTC
// audio track is mixed in WebAudio, downsampled to 16 kHz mono Int16 by an
// AudioWorklet, and sent to Go in ~1 s chunks through the deoAudio binding.
// Runs before any page script (Page.addScriptToEvaluateOnNewDocument).
(() => {
  if (window.__deoCapture) return;
  window.__deoCapture = true;

  const now = () => performance.timeOrigin + performance.now();
  const send = (obj) => { try { if (window.deoAudio) window.deoAudio(JSON.stringify(obj)); } catch (e) { /* ignore */ } };

  const worklet = `
    class DeoPCM extends AudioWorkletProcessor {
      constructor() {
        super();
        this.step = sampleRate / 16000;
        this.acc = 0; this.sum = 0; this.n = 0;
        this.out = new Int16Array(16000); this.len = 0;
      }
      process(inputs) {
        const input = inputs[0];
        if (!input || input.length === 0) return true;
        const ch = input[0];
        for (let i = 0; i < ch.length; i++) {
          let v = 0;
          for (let c = 0; c < input.length; c++) v += input[c][i];
          this.sum += v / input.length; this.n++; this.acc += 1;
          if (this.acc >= this.step) {
            this.acc -= this.step;
            const s = Math.max(-1, Math.min(1, this.sum / this.n));
            this.out[this.len++] = s < 0 ? s * 0x8000 : s * 0x7fff;
            this.sum = 0; this.n = 0;
            if (this.len === this.out.length) {
              this.port.postMessage(this.out.buffer, [this.out.buffer]);
              this.out = new Int16Array(16000); this.len = 0;
            }
          }
        }
        return true;
      }
    }
    registerProcessor("deo-pcm", DeoPCM);`;

  let ctx = null, mixer = null, ready = null;
  const sinks = []; // keep remote streams alive (Chromium only plays WebRTC audio into WebAudio when an element consumes it)

  function b64(buf) {
    const bytes = new Uint8Array(buf);
    let s = "";
    for (let i = 0; i < bytes.length; i += 0x8000) s += String.fromCharCode.apply(null, bytes.subarray(i, i + 0x8000));
    return btoa(s);
  }

  function ensure() {
    if (ready) return ready;
    ready = (async () => {
      ctx = new AudioContext();
      const url = URL.createObjectURL(new Blob([worklet], { type: "application/javascript" }));
      await ctx.audioWorklet.addModule(url);
      const node = new AudioWorkletNode(ctx, "deo-pcm", { numberOfInputs: 1, numberOfOutputs: 0 });
      mixer = ctx.createGain();
      mixer.connect(node);
      node.port.onmessage = (e) => {
        // The chunk ends now; it started 1 s earlier.
        send({ ts: now() - 1000, pcm: b64(e.data) });
      };
      send({ event: "ready", sampleRate: ctx.sampleRate });
    })();
    return ready;
  }

  async function attach(track) {
    try {
      await ensure();
      if (ctx.state !== "running") await ctx.resume();
      const stream = new MediaStream([track]);
      const el = new Audio();
      el.muted = true; el.srcObject = stream; el.play().catch(() => {});
      sinks.push(el);
      const src = ctx.createMediaStreamSource(stream);
      src.connect(mixer);
      send({ event: "track", id: track.id });
      track.addEventListener("ended", () => { try { src.disconnect(); } catch (e) { /* ignore */ } });
    } catch (e) {
      send({ event: "error", message: String(e) });
    }
  }

  const Orig = window.RTCPeerConnection;
  if (!Orig) return;
  function Patched(...args) {
    const pc = new Orig(...args);
    pc.addEventListener("track", (ev) => { if (ev.track && ev.track.kind === "audio") attach(ev.track); });
    return pc;
  }
  Patched.prototype = Orig.prototype;
  Object.setPrototypeOf(Patched, Orig);
  window.RTCPeerConnection = Patched;
  if (window.webkitRTCPeerConnection) window.webkitRTCPeerConnection = Patched;
})();
