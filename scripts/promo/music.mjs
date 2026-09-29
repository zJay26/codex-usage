import { writeFile } from "node:fs/promises";
import path from "node:path";
import { spawnSync } from "node:child_process";
import { repoRoot, run } from "../media-lib.mjs";

// A small deterministic synthesizer for the promo soundtrack. Everything is derived
// from the shared timeline so musical accents land on the visual cuts.
const rate = 48_000;
const midi = n => 440 * 2 ** ((n - 69) / 12);

function noise(seed) {
  let s = seed >>> 0;
  return () => {
    s = (s * 1664525 + 1013904223) >>> 0;
    return s / 2 ** 31 - 1;
  };
}

function writeWav(file, left, right) {
  const frames = left.length;
  const buffer = Buffer.alloc(44 + frames * 8);
  buffer.write("RIFF", 0); buffer.writeUInt32LE(36 + frames * 8, 4); buffer.write("WAVE", 8);
  buffer.write("fmt ", 12); buffer.writeUInt32LE(16, 16); buffer.writeUInt16LE(3, 20); buffer.writeUInt16LE(2, 22);
  buffer.writeUInt32LE(rate, 24); buffer.writeUInt32LE(rate * 8, 28); buffer.writeUInt16LE(8, 32); buffer.writeUInt16LE(32, 34);
  buffer.write("data", 36); buffer.writeUInt32LE(frames * 8, 40);
  for (let i = 0; i < frames; i++) { buffer.writeFloatLE(left[i], 44 + i * 8); buffer.writeFloatLE(right[i], 48 + i * 8); }
  return writeFile(file, buffer);
}

export async function renderMusic(timeline, output, workDir) {
  const length = Math.round(timeline.duration * rate);
  const beat = 60 / timeline.bpm;
  const scene = Object.fromEntries(timeline.scenes.map(s => [s.id, s]));
  // The groove runs through the feature tour: from the first feature until privacy.
  const features = timeline.scenes.slice(2, timeline.scenes.findIndex(s => s.id === "privacy"));
  const grooveStart = features[0].start, grooveEnd = scene.privacy.start, end = scene.cta.start;
  const cuts = timeline.scenes.slice(2, -1).map(s => s.start);

  const dry = [new Float32Array(length), new Float32Array(length)];
  const send = [new Float32Array(length), new Float32Array(length)];
  const duck = new Float32Array(length).fill(1);
  const add = (bus, t, fn, dur, panning = 0, sendAmount = 0) => {
    const start = Math.max(0, Math.round(t * rate)), stop = Math.min(length, Math.round((t + dur) * rate));
    const gl = Math.cos((panning + 1) * Math.PI / 4), gr = Math.sin((panning + 1) * Math.PI / 4);
    for (let i = start; i < stop; i++) {
      const v = fn((i - start) / rate, i / rate);
      bus[0][i] += v * gl; bus[1][i] += v * gr;
      if (sendAmount) { send[0][i] += v * gl * sendAmount; send[1][i] += v * gr * sendAmount; }
    }
  };

  // Harmony: a dark hook, a bright brand chord, then a four-chord loop into a resolving finale.
  const chords = { Am9: [45, 52, 57, 59, 64], C9: [48, 55, 62, 64, 71], F: [41, 53, 57, 60, 64], G: [43, 50, 59, 62, 69], Am: [45, 52, 60, 64, 67], C: [48, 55, 64, 67, 71], Gsus: [43, 50, 60, 62, 67] };
  const segments = [[0, scene.brand.start, "Am9"], [scene.brand.start, grooveStart, "C9"]];
  const loop = ["F", "G", "Am", "C"];
  let t = grooveStart;
  for (let i = 0; t < scene.privacy.start + 1; i++, t += beat * 4) segments.push([t, Math.min(t + beat * 4, scene.privacy.start + 1), loop[i % 4]]);
  segments.push([scene.privacy.start + 1, end, "Gsus"], [end, timeline.duration, "C9"]);

  // Pad: detuned additive saws with a slowly opening tone.
  for (const [a, b, name] of segments) {
    const dur = b - a + .6;
    chords[name].forEach((note, v) => {
      for (const detune of [-.0035, .0035]) {
        const f = midi(note) * (1 + detune);
        add(dry, a, (lt, at) => {
          const bright = Math.min(1, at / 20);
          const envelope = Math.min(1, lt / .35) * Math.min(1, Math.max(0, (dur - lt) / .6));
          let sum = 0;
          for (let h = 1; h <= 7; h++) sum += Math.sin(2 * Math.PI * f * h * lt + v) / h ** (2.1 - bright * .6);
          return sum * envelope * .022 * (at < scene.brand.start ? .8 : 1);
        }, dur, detune < 0 ? -.55 : .55, .45);
      }
    });
  }

  // Bass: eighth-note pulses on the chord root during the groove.
  for (const [a, b, name] of segments) {
    if (a < grooveStart || a >= grooveEnd) continue;
    const f = midi(chords[name][0] - 12);
    for (let s = a; s < Math.min(b, grooveEnd) - 1e-6; s += beat / 2) {
      add(dry, s, lt => (Math.sin(2 * Math.PI * f * lt) + .3 * Math.sin(4 * Math.PI * f * lt)) * Math.exp(-lt / .22) * Math.min(1, lt / .006) * .16, beat / 2 + .05);
    }
  }

  // Kick drum, with sidechain ducking of the other parts.
  const kick = (at, gain = 1) => {
    add(dry, at, lt => {
      const phase = 2 * Math.PI * (48 * lt + (150 - 48) * .035 * (1 - Math.exp(-lt / .035)));
      return Math.sin(phase) * Math.exp(-lt / .2) * .5 * gain;
    }, .5);
    const start = Math.round(at * rate);
    for (let i = start; i < Math.min(length, start + rate * .4); i++) duck[i] = Math.min(duck[i], 1 - .55 * Math.exp(-(i - start) / rate / .11));
  };
  kick(scene.brand.start, 1.25);
  for (let s = grooveStart; s < grooveEnd - 1e-6; s += beat) kick(s);
  kick(end, 1.25);

  // Hi-hats on the off-beats once the product tour is moving.
  const hatNoise = noise(7);
  for (let s = features[1].start + beat / 2; s < grooveEnd; s += beat) {
    let prev = 0;
    add(dry, s, lt => { const n = hatNoise(); const hp = n - prev; prev = n; return hp * Math.exp(-lt / .035) * .05; }, .12, .35);
  }

  // Pluck arpeggio with a ping-pong echo.
  const pattern = [2, 3, 4, 3, 1, 3, 4, 2];
  let step = 0;
  for (let s = grooveStart; s < grooveEnd - 1e-6; s += beat / 2, step++) {
    const [, , name] = segments.findLast(([a]) => a <= s + 1e-6);
    const f = midi(chords[name][pattern[step % pattern.length]] + 12);
    add(dry, s, lt => (Math.sin(2 * Math.PI * f * lt) + .35 * Math.sin(6 * Math.PI * f * lt) * Math.exp(-lt / .05)) * Math.exp(-lt / .18) * Math.min(1, lt / .004) * .055, .6, step % 2 ? .3 : -.3, .9);
  }

  // Bells: the brand reveal (one per rising bar) and the final resolution.
  const bell = (at, note, gain) => add(dry, at, lt => {
    const f = midi(note);
    return (Math.sin(2 * Math.PI * f * lt) * Math.exp(-lt / 1.4) + .45 * Math.sin(2 * Math.PI * f * 2.76 * lt) * Math.exp(-lt / .45) + .2 * Math.sin(2 * Math.PI * f * 5.4 * lt) * Math.exp(-lt / .18)) * Math.min(1, lt / .003) * gain;
  }, 3.2, 0, 1.2);
  [72, 76, 79, 83].forEach((note, i) => bell(scene.brand.start + .1 + i * .07, note, .06));
  [60, 67, 72, 76, 79].forEach((note, i) => bell(end + i * .06, note, .055));

  // Counter ticks during the hook, accelerating with the numbers.
  for (let s = .35, i = 0; s < scene.brand.start - .1; i++) {
    const p = s / scene.brand.start;
    add(dry, s, lt => Math.sin(2 * Math.PI * (1800 + 900 * p) * lt) * Math.exp(-lt / .012) * (.02 + .05 * p), .06, i % 2 ? .25 : -.25);
    s += .16 - .11 * p;
  }

  // Whooshes: swept band-pass noise into each cut, and a longer riser into the brand reveal.
  const whoosh = (center, pre, post, gain, seed) => {
    const n = noise(seed);
    let low = 0, band = 0;
    add(dry, center - pre, lt => {
      const p = lt / (pre + post);
      const fc = 300 + 4200 * Math.sin(Math.PI * Math.min(1, lt / pre) / 2) ** 2;
      const k = 2 * Math.sin(Math.PI * fc / rate);
      const high = n() - low - .7 * band;
      band += k * high; low += k * band;
      const envelope = lt < pre ? (lt / pre) ** 2 : Math.exp(-(lt - pre) / (post / 3));
      return band * envelope * gain * (1 - p * .2);
    }, pre + post, 0, .6);
  };
  whoosh(scene.brand.start, 2.0, .35, .12, 11);
  cuts.forEach((cut, i) => whoosh(cut, .45, .3, .05, 20 + i));
  whoosh(end, 1.4, .4, .09, 99);

  // Send bus: ping-pong delay followed by a light comb reverb.
  const wet = [new Float32Array(length), new Float32Array(length)];
  const dl = Math.round(beat * .75 * rate), dr = Math.round(beat * rate);
  for (let i = 0; i < length; i++) {
    const fl = i >= dl ? wet[1][i - dl] * .38 : 0, fr = i >= dr ? wet[0][i - dr] * .38 : 0;
    wet[0][i] = send[0][i] + fr * .9 + fl * .1;
    wet[1][i] = send[1][i] + fl * .9 + fr * .1;
  }
  for (const ch of [0, 1]) {
    const combs = (ch ? [1617, 1557, 1491, 1422] : [1557, 1617, 1422, 1491]).map(n => ({ buf: new Float32Array(Math.round(n * rate / 44100)), i: 0, low: 0 }));
    for (let i = 0; i < length; i++) {
      let r = 0;
      for (const c of combs) {
        const out = c.buf[c.i];
        c.low = out * .7 + c.low * .3;
        c.buf[c.i] = wet[ch][i] * .2 + c.low * .78;
        c.i = (c.i + 1) % c.buf.length;
        r += out;
      }
      wet[ch][i] = wet[ch][i] * .55 + r * .25;
    }
  }

  // Mix, duck, soft-clip, and fade.
  const left = new Float32Array(length), right = new Float32Array(length);
  for (let i = 0; i < length; i++) {
    const s = i / rate;
    const fade = Math.min(1, s / .05) * Math.min(1, (timeline.duration - s) / 1.6);
    left[i] = Math.tanh((dry[0][i] * duck[i] + wet[0][i] * (.5 + .5 * duck[i])) * 1.3) * fade;
    right[i] = Math.tanh((dry[1][i] * duck[i] + wet[1][i] * (.5 + .5 * duck[i])) * 1.3) * fade;
  }
  const raw = path.join(workDir, "music-raw.wav");
  await writeWav(raw, left, right);

  // Two-pass EBU R128 loudness normalization for consistent playback volume.
  const target = "I=-16:TP=-1.5:LRA=11";
  // loudnorm reports its measurement on stderr.
  const analysis = spawnSync("ffmpeg", ["-hide_banner", "-nostats", "-i", raw, "-af", `loudnorm=${target}:print_format=json`, "-f", "null", "-"], { cwd: repoRoot, encoding: "utf8", windowsHide: true });
  const measured = JSON.parse(analysis.stderr.match(/\{[^{}]*"input_i"[^{}]*\}/)[0]);
  const filter = `loudnorm=${target}:measured_I=${measured.input_i}:measured_TP=${measured.input_tp}:measured_LRA=${measured.input_lra}:measured_thresh=${measured.input_thresh}:offset=${measured.target_offset}:linear=true`;
  run("ffmpeg", ["-hide_banner", "-loglevel", "error", "-y", "-i", raw, "-af", `${filter},aresample=${rate}`, "-c:a", "pcm_s16le", output]);
}
