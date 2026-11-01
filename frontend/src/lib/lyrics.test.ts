import { describe, expect, it } from 'vitest';
import mockTranscript from '../../../mocks/transcript.json';
import { focusedLineIndex, formatTime, groupWordsIntoLines, isWordActive, parseTranscript, wordProgress, type TimedWord } from './lyrics';

const word = (text: string, start: number, end = start + 0.4): TimedWord => ({ text, start, end, confidence: null });

describe('groupWordsIntoLines', () => {
  it('preserves every original mock word, its order and timestamps', () => {
    const transcript = parseTranscript(mockTranscript);
    const lines = groupWordsIntoLines(transcript.words);
    const flattened = lines.flatMap(line => line.words);
    expect(flattened).toHaveLength(331);
    expect(flattened).toEqual(transcript.words);
    flattened.forEach((item, index) => expect(item).toBe(transcript.words[index]));
    expect(lines.every(line => line.words.length > 0 && line.words.length <= 8)).toBe(true);
    expect(lines[0]?.start).toBe(0.16);
    expect(lines.at(-1)?.end).toBe(121.1);
  });

  it('breaks at sentence endings, including punctuation followed by quotes', () => {
    const words = [word('Hello', 0), word('world!”', 0.5), word('Next?', 1), word('Yes.', 1.5)];
    expect(groupWordsIntoLines(words).map(line => line.words.map(item => item.text))).toEqual([['Hello', 'world!”'], ['Next?'], ['Yes.']]);
  });

  it('breaks at a pause of at least 0.8 seconds', () => {
    expect(groupWordsIntoLines([word('Before', 0, 0.2), word('after', 1)]).map(line => line.words.length)).toEqual([1, 1]);
    expect(groupWordsIntoLines([word('No', 0, 0.2), word('break', 0.99)])).toHaveLength(1);
  });

  it('splits long phrases into groups of eight and flushes the last word', () => {
    const lines = groupWordsIntoLines(Array.from({ length: 17 }, (_, index) => word(String(index), index * 0.5)));
    expect(lines.map(line => line.words.length)).toEqual([8, 8, 1]);
  });

  it('supports an empty transcript', () => {
    expect(groupWordsIntoLines([])).toEqual([]);
    expect(focusedLineIndex([], 10)).toBe(-1);
  });
});

describe('audio-time synchronization', () => {
  const first = word('First', 1, 2);
  const second = word('second', 2, 3);

  it('fills each word continuously and clamps outside its interval', () => {
    expect([0, 1, 1.25, 1.5, 2, 20].map(time => wordProgress(first, time))).toEqual([0, 0, 0.25, 0.5, 1, 1]);
  });

  it('includes the start and excludes the end, avoiding double highlights', () => {
    expect(isWordActive(first, 1)).toBe(true);
    expect(isWordActive(first, 2)).toBe(false);
    expect(isWordActive(second, 2)).toBe(true);
  });

  it('does not highlight a word in leading silence, pauses or the audio tail', () => {
    const transcript = parseTranscript(mockTranscript);
    const gap = transcript.words.find((item, index) => {
      const next = transcript.words[index + 1];
      return next && next.start - item.end > 1;
    });
    expect(gap).toBeDefined();
    for (const time of [0, (gap?.end ?? 0) + 0.5, 125, 131.89]) {
      expect(transcript.words.some(item => isWordActive(item, time))).toBe(false);
    }
    expect(wordProgress(transcript.words.at(-1)!, 125)).toBe(1);
  });

  it('recomputes progress and focused line after seeking both forward and backward', () => {
    const lines = groupWordsIntoLines([word('First.', 1, 2), word('Second.', 4, 5), word('Third.', 8, 9)]);
    expect([0, 4.5, 8.5, 1.5, 6, 12].map(time => focusedLineIndex(lines, time))).toEqual([0, 1, 2, 0, 1, 2]);
    expect(wordProgress(first, 8)).toBe(1);
    expect(wordProgress(first, 1.5)).toBe(0.5);
  });
});

describe('transcript input and time formatting', () => {
  it('accepts empty words and absent confidence; keeps time in seconds', () => {
    expect(parseTranscript({ schema_version: 1, text: '', words: [] }).words).toEqual([]);
    expect(parseTranscript({ schema_version: 1, text: 'Hi', words: [{ text: 'Hi', start: 0.16, end: 0.6 }] }).words[0]).toEqual(word('Hi', 0.16, 0.6));
  });

  it.each([
    null,
    { schema_version: 2, text: '', words: [] },
    { schema_version: 1, text: '', words: [{ text: 'Bad', start: 2, end: 1 }] },
    { schema_version: 1, text: '', words: [{ text: 'Bad', start: NaN, end: 1 }] },
    { schema_version: 1, text: '', words: [word('Later', 2), word('Earlier', 1)] },
  ])('rejects malformed input: %j', input => expect(() => parseTranscript(input)).toThrow());

  it('formats actual audio time without inventing a duration from the transcript', () => {
    expect([0, 9.9, 60, 121.1, 131.889, Infinity].map(formatTime)).toEqual(['0:00', '0:09', '1:00', '2:01', '2:11', '0:00']);
  });
});
