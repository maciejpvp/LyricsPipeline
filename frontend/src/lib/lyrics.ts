export interface TimedWord {
  readonly text: string;
  readonly start: number;
  readonly end: number;
  readonly confidence: number | null;
}

export interface Transcript {
  readonly schema_version: 1;
  readonly text: string;
  readonly words: readonly TimedWord[];
}

export interface LyricLine {
  readonly id: number;
  readonly start: number;
  readonly end: number;
  readonly words: readonly TimedWord[];
}

const MAX_WORDS_PER_LINE = 8;
const LINE_BREAK_GAP_SECONDS = 0.8;
const SENTENCE_END = /[.!?]["'”’)]*$/u;

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === 'object' && value !== null && !Array.isArray(value);
}

function isFiniteNumber(value: unknown): value is number {
  return typeof value === 'number' && Number.isFinite(value);
}

/** Accept the pipeline's v1 contract, without using S3 metadata as a media URL. */
export function parseTranscript(value: unknown): Transcript {
  if (!isRecord(value) || value.schema_version !== 1 || typeof value.text !== 'string' || !Array.isArray(value.words)) {
    throw new Error('Nieprawidłowy format transkrypcji. Oczekiwano schema_version: 1 i tablicy words.');
  }

  let previousStart = -Infinity;
  const words = value.words.map((word: unknown): TimedWord => {
    if (!isRecord(word) || typeof word.text !== 'string' || !word.text.trim()
      || !isFiniteNumber(word.start) || !isFiniteNumber(word.end)
      || word.start < 0 || word.end <= word.start || word.start < previousStart
      || (word.confidence != null && (!isFiniteNumber(word.confidence) || word.confidence < 0 || word.confidence > 1))) {
      throw new Error('Transkrypcja zawiera nieprawidłowe słowa lub znaczniki czasu.');
    }
    previousStart = word.start;
    return { text: word.text, start: word.start, end: word.end, confidence: word.confidence ?? null };
  });

  return { schema_version: 1, text: value.text, words };
}

/** Group the original word objects; never estimate or modify their timestamps. */
export function groupWordsIntoLines(words: readonly TimedWord[]): LyricLine[] {
  const lines: LyricLine[] = [];
  let pending: TimedWord[] = [];

  const flush = () => {
    const first = pending[0];
    const last = pending.at(-1);
    if (!first || !last) return;
    lines.push({ id: lines.length, start: first.start, end: last.end, words: pending });
    pending = [];
  };

  for (const word of words) {
    const previous = pending.at(-1);
    if (previous && word.start - previous.end >= LINE_BREAK_GAP_SECONDS) flush();
    pending.push(word);
    if (pending.length >= MAX_WORDS_PER_LINE || SENTENCE_END.test(word.text)) flush();
  }
  flush();
  return lines;
}

export function wordProgress(word: TimedWord, currentTime: number): number {
  return Math.max(0, Math.min(1, (currentTime - word.start) / (word.end - word.start)));
}

export function isWordActive(word: TimedWord, currentTime: number): boolean {
  return word.start <= currentTime && currentTime < word.end;
}

/** Keep the most recently started line in view across pauses and the audio tail. */
export function focusedLineIndex(lines: readonly LyricLine[], currentTime: number): number {
  if (!lines.length) return -1;
  let low = 0;
  let high = lines.length - 1;
  let result = 0;
  while (low <= high) {
    const middle = Math.floor((low + high) / 2);
    const line = lines[middle];
    if (line && line.start <= currentTime) {
      result = middle;
      low = middle + 1;
    } else {
      high = middle - 1;
    }
  }
  return result;
}

export function formatTime(seconds: number): string {
  const whole = Number.isFinite(seconds) ? Math.max(0, Math.floor(seconds)) : 0;
  return `${Math.floor(whole / 60)}:${String(whole % 60).padStart(2, '0')}`;
}
