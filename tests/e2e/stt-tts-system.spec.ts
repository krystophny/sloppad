import { expect, test } from './live';
import {
  WS_URL,
  authenticate,
  getChatSessionId,
  openRawWS,
  buildWavSilence,
  buildWavSineWave,
  synthesizePiperWav,
  transcodeWavToM4A,
  postSTTTranscribeAPI,
} from './helpers';

function mergeWavChunks(chunks: Buffer[]): Buffer {
  expect(chunks.length).toBeGreaterThan(0);
  const first = chunks[0]!;
  const channels = first.readUInt16LE(22);
  const sampleRate = first.readUInt32LE(24);
  const bitsPerSample = first.readUInt16LE(34);
  const pcmParts = chunks.map((chunk) => chunk.slice(44));
  const dataSize = pcmParts.reduce((sum, part) => sum + part.length, 0);
  const out = Buffer.alloc(44 + dataSize);
  out.write('RIFF', 0);
  out.writeUInt32LE(36 + dataSize, 4);
  out.write('WAVE', 8);
  out.write('fmt ', 12);
  out.writeUInt32LE(16, 16);
  out.writeUInt16LE(1, 20);
  out.writeUInt16LE(channels, 22);
  out.writeUInt32LE(sampleRate, 24);
  const bytesPerSample = bitsPerSample / 8;
  out.writeUInt32LE(sampleRate * channels * bytesPerSample, 28);
  out.writeUInt16LE(channels * bytesPerSample, 32);
  out.writeUInt16LE(bitsPerSample, 34);
  out.write('data', 36);
  out.writeUInt32LE(dataSize, 40);
  let offset = 44;
  for (const part of pcmParts) {
    part.copy(out, offset);
    offset += part.length;
  }
  return out;
}

test.describe('STT/TTS system tests @local-only', () => {
  let sessionToken: string;
  let chatSessionId: string;

  test.beforeAll(async () => {
    sessionToken = await authenticate();
    chatSessionId = await getChatSessionId(sessionToken);
  });

  test.describe('TTS', () => {
    test('TTS returns WAV audio with valid RIFF header', async () => {
      const conn = await openRawWS(`${WS_URL}/ws/chat/${chatSessionId}`, sessionToken);
      try {
        conn.ws.send(JSON.stringify({ type: 'tts_speak', text: 'Hello world.', lang: 'en' }));
        const wav = await conn.waitForBinary(15_000);
        expect(wav.length).toBeGreaterThan(44);
        expect(wav.slice(0, 4).toString('ascii')).toBe('RIFF');
        expect(wav.slice(8, 12).toString('ascii')).toBe('WAVE');
      } finally {
        conn.close();
      }
    });

    test('TTS handles multiple sentences in order', async () => {
      const conn = await openRawWS(`${WS_URL}/ws/chat/${chatSessionId}`, sessionToken);
      try {
        const sentences = ['First sentence.', 'Second sentence.', 'Third sentence.'];
        for (const text of sentences) {
          conn.ws.send(JSON.stringify({ type: 'tts_speak', text, lang: 'en' }));
        }

        const wavBuffers: Buffer[] = [];
        for (let i = 0; i < sentences.length; i++) {
          const before = conn.messages.filter((m): m is { kind: 'binary'; data: Buffer } => m.kind === 'binary').length;
          await conn.waitForText((m) => m.type === 'tts_done' && Number(m.seq) === i, 20_000);
          const binaries = conn.messages.filter((m): m is { kind: 'binary'; data: Buffer } => m.kind === 'binary');
          expect(binaries.length).toBeGreaterThan(before);
          const wav = binaries[before]!.data;
          wavBuffers.push(wav);
        }

        expect(wavBuffers).toHaveLength(3);
        for (const wav of wavBuffers) {
          expect(wav.length).toBeGreaterThan(44);
          expect(wav.slice(0, 4).toString('ascii')).toBe('RIFF');
        }
      } finally {
        conn.close();
      }
    });
  });

  test.describe('STT', () => {
    test('STT rejects short audio as too short', async () => {
      const conn = await openRawWS(`${WS_URL}/ws/chat/${chatSessionId}`, sessionToken);
      try {
        conn.ws.send(JSON.stringify({ type: 'stt_start', mime_type: 'audio/wav' }));
        await conn.waitForText((m) => m.type === 'stt_started', 5_000);

        conn.ws.send(buildWavSilence(10));
        conn.ws.send(JSON.stringify({ type: 'stt_stop' }));

        const result = await conn.waitForText(
          (m) => m.type === 'stt_empty' || m.type === 'stt_result' || m.type === 'stt_error',
          10_000,
        );
        expect(result.type).toBe('stt_empty');
        expect(result.reason).toBe('recording_too_short');
      } finally {
        conn.close();
      }
    });

    test('STT returns empty for silence', async () => {
      const conn = await openRawWS(`${WS_URL}/ws/chat/${chatSessionId}`, sessionToken);
      try {
        conn.ws.send(JSON.stringify({ type: 'stt_start', mime_type: 'audio/wav' }));
        await conn.waitForText((m) => m.type === 'stt_started', 5_000);

        conn.ws.send(buildWavSilence(2000));
        conn.ws.send(JSON.stringify({ type: 'stt_stop' }));

        const result = await conn.waitForText(
          (m) => m.type === 'stt_empty' || m.type === 'stt_result' || m.type === 'stt_error',
          15_000,
        );
        expect(['stt_empty', 'stt_error']).toContain(result.type);
      } finally {
        conn.close();
      }
    });

    test('STT cancel discards audio', async () => {
      const conn = await openRawWS(`${WS_URL}/ws/chat/${chatSessionId}`, sessionToken);
      try {
        conn.ws.send(JSON.stringify({ type: 'stt_start', mime_type: 'audio/wav' }));
        await conn.waitForText((m) => m.type === 'stt_started', 5_000);

        conn.ws.send(buildWavSineWave(1000));
        conn.ws.send(JSON.stringify({ type: 'stt_cancel' }));

        const result = await conn.waitForText((m) => m.type === 'stt_cancelled', 5_000);
        expect(result.type).toBe('stt_cancelled');
      } finally {
        conn.close();
      }
    });
  });

  test.describe('STT+TTS round-trip', () => {
    test('TTS-generated audio round-trips through STT', async () => {
      const ttsConn = await openRawWS(`${WS_URL}/ws/chat/${chatSessionId}`, sessionToken);
      let ttsWav: Buffer;
      try {
        ttsConn.ws.send(JSON.stringify({ type: 'tts_speak', text: 'The quick brown fox jumps over the lazy dog.', lang: 'en' }));
        const chunks: Buffer[] = [];
        const seen = new Set<number>();
        for (;;) {
          const donePromise = ttsConn.waitForText((m) => m.type === 'tts_done', 20_000);
          let grew = true;
          while (grew) {
            grew = false;
            const binaries = ttsConn.messages.filter((m): m is { kind: 'binary'; data: Buffer } => m.kind === 'binary');
            for (let i = 0; i < binaries.length; i++) {
              if (seen.has(i)) continue;
              seen.add(i);
              chunks.push(binaries[i]!.data);
              grew = true;
            }
            if (!grew) {
              await new Promise((resolve) => setTimeout(resolve, 50));
            }
            if (chunks.length > 0) break;
          }
          await donePromise;
          const binaries = ttsConn.messages.filter((m): m is { kind: 'binary'; data: Buffer } => m.kind === 'binary');
          for (let i = 0; i < binaries.length; i++) {
            if (seen.has(i)) continue;
            seen.add(i);
            chunks.push(binaries[i]!.data);
          }
          expect(chunks.length).toBeGreaterThan(0);
          ttsWav = mergeWavChunks(chunks);
          break;
        }
        expect(ttsWav.length).toBeGreaterThan(44);
      } finally {
        ttsConn.close();
      }

      const sttConn = await openRawWS(`${WS_URL}/ws/chat/${chatSessionId}`, sessionToken);
      try {
        sttConn.ws.send(JSON.stringify({ type: 'stt_start', mime_type: 'audio/wav' }));
        await sttConn.waitForText((m) => m.type === 'stt_started', 5_000);

        sttConn.ws.send(ttsWav);
        sttConn.ws.send(JSON.stringify({ type: 'stt_stop' }));

        const result = await sttConn.waitForText(
          (m) => m.type === 'stt_result' || m.type === 'stt_empty' || m.type === 'stt_error',
          60_000,
        );
        expect(result.type).toBe('stt_result');
        const transcript = String(result.text || '').toLowerCase();
        expect(transcript).toBeTruthy();
        expect(transcript.length).toBeGreaterThan(5);
      } finally {
        sttConn.close();
      }
    });

    test('Piper-generated WAV round-trips through authenticated /api/stt/transcribe', async () => {
      const wav = await synthesizePiperWav('Tabura end to end speech to text verification.');
      expect(wav.length).toBeGreaterThan(44);
      expect(wav.slice(0, 4).toString('ascii')).toBe('RIFF');

      const { status, payload, raw } = await postSTTTranscribeAPI(sessionToken, 'audio/wav', wav);
      expect(status, raw).toBe(200);
      const text = String(payload.text || '').trim();
      expect(text.length).toBeGreaterThan(0);
    });

    test('Piper-generated M4A round-trips through authenticated /api/stt/transcribe', async () => {
      const wav = await synthesizePiperWav('Tabura m4a normalization to stt verification.');
      const m4a = transcodeWavToM4A(wav);
      expect(m4a.length).toBeGreaterThan(512);

      const { status, payload, raw } = await postSTTTranscribeAPI(sessionToken, 'audio/mp4', m4a);
      expect(status, raw).toBe(200);
      const text = String(payload.text || '').trim();
      expect(text.length).toBeGreaterThan(0);
    });
  });
});
