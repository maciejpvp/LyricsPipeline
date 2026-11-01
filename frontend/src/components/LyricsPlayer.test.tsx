// @vitest-environment happy-dom
import { act, cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import mockTranscript from '../../../mocks/transcript.json';
import LyricsPlayer from './LyricsPlayer';

const fetchMock = vi.fn();
const playMock = vi.fn();
const pauseMock = vi.fn();
const loadMock = vi.fn();

beforeEach(() => {
  fetchMock.mockResolvedValue(new Response(JSON.stringify(mockTranscript)));
  vi.stubGlobal('fetch', fetchMock);
  vi.stubGlobal('matchMedia', () => ({ matches: true }));
  vi.spyOn(HTMLElement.prototype, 'scrollTo').mockImplementation(() => {});
  vi.spyOn(HTMLMediaElement.prototype, 'play').mockImplementation(playMock);
  vi.spyOn(HTMLMediaElement.prototype, 'pause').mockImplementation(pauseMock);
  vi.spyOn(HTMLMediaElement.prototype, 'load').mockImplementation(loadMock);
  playMock.mockResolvedValue(undefined);
});

afterEach(() => {
  cleanup();
  vi.restoreAllMocks();
  vi.unstubAllGlobals();
  vi.resetAllMocks();
});

async function renderPlayer() {
  const result = render(<LyricsPlayer />);
  await screen.findByRole('button', { name: /^Przejdź do 0:00:/ });
  const audio = result.container.querySelector('audio')!;
  Object.defineProperty(audio, 'duration', { configurable: true, value: 131.889 });
  fireEvent.loadedMetadata(audio);
  return { ...result, audio };
}

describe('player interactions', () => {
  it('reads metadata that finished loading before React attached its listeners', async () => {
    vi.spyOn(HTMLMediaElement.prototype, 'duration', 'get').mockReturnValue(131.889);
    vi.spyOn(HTMLMediaElement.prototype, 'readyState', 'get').mockReturnValue(4);
    render(<LyricsPlayer />);
    await screen.findByRole('button', { name: /^Przejdź do 0:00:/ });
    const timeline = screen.getByRole('slider', { name: 'Pozycja odtwarzania' });
    expect(timeline.getAttribute('max')).toBe('131.889');
    expect((timeline as HTMLInputElement).disabled).toBe(false);
    expect(screen.getByText('2:11')).toBeDefined();
  });

  it.each([NaN, Infinity, 0])('keeps the timeline unavailable for duration %s, without blocking play', async duration => {
    const { container } = render(<LyricsPlayer />);
    await screen.findByRole('button', { name: /^Przejdź do 0:00:/ });
    const audio = container.querySelector('audio')!;
    Object.defineProperty(audio, 'duration', { configurable: true, value: duration });
    fireEvent.canPlay(audio);
    expect((screen.getByRole('button', { name: 'Odtwórz' }) as HTMLButtonElement).disabled).toBe(false);
    expect((screen.getByRole('slider', { name: 'Pozycja odtwarzania' }) as HTMLInputElement).disabled).toBe(true);
    expect(screen.getByText('—:—')).toBeDefined();
  });

  it('recovers a duration missed at startup on the next media update', async () => {
    const { container } = render(<LyricsPlayer />);
    await screen.findByRole('button', { name: /^Przejdź do 0:00:/ });
    const audio = container.querySelector('audio')!;
    Object.defineProperty(audio, 'duration', { configurable: true, value: 131.889 });
    audio.currentTime = 2;
    fireEvent.timeUpdate(audio);
    const timeline = screen.getByRole('slider', { name: 'Pozycja odtwarzania' });
    expect(timeline.getAttribute('max')).toBe('131.889');
    expect(timeline.getAttribute('aria-valuetext')).toBe('0:02 z 2:11');
    expect((timeline as HTMLInputElement).value).toBe('2');
  });

  it('loads local media and takes duration and word progress from the audio element', async () => {
    const { audio, container } = await renderPlayer();
    expect(audio.getAttribute('src')).toBe('/rick.mp3');
    expect(fetchMock).toHaveBeenCalledWith('/transcript.json', expect.anything());
    expect(audio.autoplay).toBe(false);
    expect(screen.getByRole('slider', { name: 'Pozycja odtwarzania' }).getAttribute('max')).toBe('131.889');

    audio.currentTime = 0.38;
    fireEvent.timeUpdate(audio);
    const active = container.querySelector('[data-active="true"]');
    expect(active?.textContent).toBe(mockTranscript.words[0]?.text);
    expect(parseFloat((active as HTMLElement).style.getPropertyValue('--word-progress'))).toBeCloseTo(50);

    audio.currentTime = 125;
    fireEvent.timeUpdate(audio);
    expect(container.querySelector('[data-active="true"]')).toBeNull();
    expect(screen.getByRole('slider', { name: 'Pozycja odtwarzania' }).getAttribute('aria-valuetext')).toBe('2:05 z 2:11');
  });

  it('plays only on user action, pauses and preserves the actual audio clock', async () => {
    const { audio } = await renderPlayer();
    expect(playMock).not.toHaveBeenCalled();
    fireEvent.click(screen.getByRole('button', { name: 'Odtwórz' }));
    await waitFor(() => expect(playMock).toHaveBeenCalledOnce());
    Object.defineProperty(audio, 'paused', { configurable: true, value: false });
    fireEvent.play(audio);
    fireEvent.click(screen.getByRole('button', { name: 'Wstrzymaj' }));
    expect(pauseMock).toHaveBeenCalledOnce();
    audio.currentTime = 7;
    Object.defineProperty(audio, 'paused', { configurable: true, value: true });
    fireEvent.pause(audio);
    expect(screen.getByRole('button', { name: 'Odtwórz' })).toBeDefined();
    expect(screen.getByRole('slider', { name: 'Pozycja odtwarzania' }).getAttribute('aria-valuetext')).toBe('0:07 z 2:11');
  });

  it('does not turn an interrupted play request into a persistent error', async () => {
    await renderPlayer();
    let rejectPlay!: (reason: unknown) => void;
    playMock.mockImplementationOnce(() => new Promise<void>((_, reject) => { rejectPlay = reject; }));
    fireEvent.click(screen.getByRole('button', { name: 'Odtwórz' }));
    fireEvent.click(screen.getByRole('button', { name: 'Odtwórz' }));
    expect(pauseMock).toHaveBeenCalledOnce();
    await act(async () => rejectPlay(new DOMException('Interrupted by pause', 'AbortError')));
    expect(screen.queryByRole('alert')).toBeNull();
    expect((screen.getByRole('button', { name: 'Odtwórz' }) as HTMLButtonElement).disabled).toBe(false);
    fireEvent.click(screen.getByRole('button', { name: 'Odtwórz' }));
    await waitFor(() => expect(playMock).toHaveBeenCalledTimes(2));
  });

  it('bounds skip controls to the recording and restarts after the end', async () => {
    const { audio } = await renderPlayer();
    audio.currentTime = 2;
    fireEvent.click(screen.getByRole('button', { name: 'Cofnij o 5 sekund' }));
    expect(audio.currentTime).toBe(0);
    audio.currentTime = 130;
    fireEvent.click(screen.getByRole('button', { name: 'Przewiń o 5 sekund' }));
    expect(audio.currentTime).toBe(131.889);
    Object.defineProperty(audio, 'ended', { configurable: true, value: true });
    fireEvent.ended(audio);
    fireEvent.click(screen.getByRole('button', { name: 'Odtwórz' }));
    await waitFor(() => expect(playMock).toHaveBeenCalledOnce());
    expect(audio.currentTime).toBe(0);
  });

  it('reflects external media volume changes in the controls', async () => {
    const { audio } = await renderPlayer();
    audio.volume = 0.4;
    audio.muted = true;
    fireEvent.volumeChange(audio);
    expect(screen.getByRole('button', { name: 'Włącz dźwięk' }).getAttribute('aria-pressed')).toBe('true');
    expect((screen.getByRole('slider', { name: 'Głośność' }) as HTMLInputElement).value).toBe('0.4');
  });

  it('seeks from lyric buttons and the timeline, and suspends/resumes following', async () => {
    const { audio } = await renderPlayer();
    const region = screen.getByRole('region', { name: /^Tekst nagrania/ });
    fireEvent.wheel(region, { deltaY: 200 });
    expect(screen.getByRole('button', { name: 'Wróć do tekstu' })).toBeDefined();
    fireEvent.click(screen.getByRole('button', { name: 'Wróć do tekstu' }));
    expect(screen.queryByRole('button', { name: 'Wróć do tekstu' })).toBeNull();

    fireEvent.click(screen.getByRole('button', { name: /^Przejdź do 0:01:/ }));
    expect(audio.currentTime).toBe(1.88);
    const timeline = screen.getByRole('slider', { name: 'Pozycja odtwarzania' });
    fireEvent.change(timeline, { target: { value: '60' } });
    expect(audio.currentTime).toBe(60);
    fireEvent.change(timeline, { target: { value: '1' } });
    expect(audio.currentTime).toBe(1);

    fireEvent.keyDown(region, { key: 'PageDown' });
    expect(screen.getByRole('button', { name: 'Wróć do tekstu' })).toBeDefined();
  });

  it('changes volume, mutes and restores audio even after setting volume to zero', async () => {
    const { audio } = await renderPlayer();
    fireEvent.change(screen.getByRole('slider', { name: 'Głośność' }), { target: { value: '0.3' } });
    expect(audio.volume).toBe(0.3);
    fireEvent.click(screen.getByRole('button', { name: 'Wycisz' }));
    expect(audio.muted).toBe(true);
    fireEvent.click(screen.getByRole('button', { name: 'Włącz dźwięk' }));
    expect(audio.muted).toBe(false);
    expect(audio.volume).toBe(0.3);
    fireEvent.change(screen.getByRole('slider', { name: 'Głośność' }), { target: { value: '0' } });
    fireEvent.click(screen.getByRole('button', { name: 'Włącz dźwięk' }));
    expect(audio.volume).toBeGreaterThan(0);
    expect(audio.muted).toBe(false);
  });

  it('shows an audio error and retries loading', async () => {
    const { audio } = await renderPlayer();
    fireEvent.error(audio);
    expect(screen.getByRole('alert').textContent).toContain('Nie udało się wczytać rick.mp3');
    fireEvent.click(screen.getByRole('button', { name: 'Ponów' }));
    expect(loadMock).toHaveBeenCalledOnce();
    expect(screen.queryByRole('alert')).toBeNull();
  });

  it('handles an empty transcript while keeping the audio controls available', async () => {
    fetchMock.mockResolvedValue(new Response(JSON.stringify({ schema_version: 1, text: '', words: [] })));
    const { container } = render(<LyricsPlayer />);
    await screen.findByText('Tu gra sam dźwięk.');
    const audio = container.querySelector('audio')!;
    fireEvent.canPlay(audio);
    expect((screen.getByRole('button', { name: 'Odtwórz' }) as HTMLButtonElement).disabled).toBe(false);
  });

  it('shows loading and offers a retry after a transcript request fails', async () => {
    fetchMock.mockRejectedValueOnce(new Error('Brak połączenia.'));
    render(<LyricsPlayer />);
    expect(screen.getByText('Wczytywanie tekstu nagrania…')).toBeDefined();
    await screen.findByRole('alert');
    fetchMock.mockResolvedValueOnce(new Response(JSON.stringify(mockTranscript)));
    await act(async () => fireEvent.click(screen.getByRole('button', { name: 'Spróbuj ponownie' })));
    expect(await screen.findByRole('button', { name: /^Przejdź do 0:00:/ })).toBeDefined();
    expect(screen.queryByRole('alert')).toBeNull();
  });
});
