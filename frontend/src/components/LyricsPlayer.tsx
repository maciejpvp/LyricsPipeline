import { memo, useCallback, useEffect, useMemo, useRef, useState, type CSSProperties, type KeyboardEvent } from 'react';
import { focusedLineIndex, formatTime, groupWordsIntoLines, isWordActive, parseTranscript, wordProgress, type LyricLine, type Transcript } from '../lib/lyrics';
import { useAudioPlayer } from '../hooks/useAudioPlayer';
import Icon from './Icon';

const MEDIA_BASE = import.meta.env.BASE_URL;
const SEEK_STEP_SECONDS = 5;
const FOLLOW_POSITION_RATIO = 0.42;
const SCROLL_KEYS = new Set(['ArrowDown', 'ArrowUp', 'PageDown', 'PageUp', 'Home', 'End', ' ']);

type TranscriptState =
  | { status: 'loading' }
  | { status: 'ready'; transcript: Transcript }
  | { status: 'error'; message: string };

const LyricRow = memo(function LyricRow({ line, currentTime, focused, past, onSeek }: {
  line: LyricLine;
  currentTime: number;
  focused: boolean;
  past: boolean;
  onSeek: (time: number) => void;
}) {
  return (
    <button
      type="button"
      className={`lyric-line${focused ? ' lyric-line--focused' : ''}${past ? ' lyric-line--past' : ''}`}
      data-line-id={line.id}
      aria-current={focused ? 'true' : undefined}
      aria-label={`Przejdź do ${formatTime(line.start)}: ${line.words.map(word => word.text).join(' ')}`}
      onClick={() => onSeek(line.start)}
    >
      {line.words.map((word, index) => (
        <span key={`${word.start}-${index}`} className="word-wrap">
          <span
            className="lyric-word"
            style={{ '--word-progress': `${wordProgress(word, currentTime) * 100}%` } as CSSProperties}
            data-active={isWordActive(word, currentTime) || undefined}
          >{word.text}</span>{index < line.words.length - 1 ? ' ' : ''}
        </span>
      ))}
    </button>
  );
});

export default function LyricsPlayer() {
  const audioRef = useRef<HTMLAudioElement>(null);
  const scrollerRef = useRef<HTMLDivElement>(null);
  const [transcriptState, setTranscriptState] = useState<TranscriptState>({ status: 'loading' });
  const [loadAttempt, setLoadAttempt] = useState(0);
  const [following, setFollowing] = useState(true);
  const audio = useAudioPlayer(audioRef);
  const { currentTime, duration, playing, buffering, ready, volume, muted, error, seek } = audio;

  const lines = useMemo(() => transcriptState.status === 'ready'
    ? groupWordsIntoLines(transcriptState.transcript.words) : [], [transcriptState]);
  const focusedIndex = focusedLineIndex(lines, currentTime);
  const hasWords = lines.length > 0;
  const canSeek = duration !== null && !error;
  const durationLabel = duration === null ? '—:—' : formatTime(duration);

  useEffect(() => {
    const controller = new AbortController();
    setTranscriptState({ status: 'loading' });
    void fetch(`${MEDIA_BASE}transcript.json`, { signal: controller.signal })
      .then(async response => {
        if (!response.ok) throw new Error(`Nie udało się pobrać tekstu (HTTP ${response.status}).`);
        return parseTranscript(await response.json());
      })
      .then(transcript => {
        if (!controller.signal.aborted) setTranscriptState({ status: 'ready', transcript });
      })
      .catch((cause: unknown) => {
        if (!controller.signal.aborted) setTranscriptState({
          status: 'error',
          message: cause instanceof Error ? cause.message : 'Nie udało się wczytać tekstu.',
        });
      });
    return () => controller.abort();
  }, [loadAttempt]);

  const centerLine = useCallback((behavior: ScrollBehavior = 'smooth') => {
    const scroller = scrollerRef.current;
    const line = scroller?.querySelector<HTMLElement>(`[data-line-id="${focusedIndex}"]`);
    if (!scroller || !line) return;
    const reducedMotion = window.matchMedia('(prefers-reduced-motion: reduce)').matches;
    const top = scroller.scrollTop + line.getBoundingClientRect().top - scroller.getBoundingClientRect().top
      - scroller.clientHeight * FOLLOW_POSITION_RATIO + line.clientHeight / 2;
    scroller.scrollTo({ top: Math.max(0, top), behavior: reducedMotion ? 'instant' : behavior });
  }, [focusedIndex]);

  useEffect(() => {
    if (following) centerLine();
  }, [following, centerLine]);

  useEffect(() => {
    if (!following) return;
    const recenter = () => centerLine('instant');
    window.addEventListener('resize', recenter);
    return () => window.removeEventListener('resize', recenter);
  }, [following, centerLine]);

  const seekToLine = useCallback((time: number) => {
    seek(time);
    setFollowing(true);
  }, [seek]);

  const handleScrollerKey = (event: KeyboardEvent<HTMLDivElement>) => {
    if (event.key === ' ' && event.target instanceof HTMLButtonElement) return;
    if (SCROLL_KEYS.has(event.key)) setFollowing(false);
  };

  const progressStyle = { '--range-progress': `${duration ? Math.min(currentTime / duration, 1) * 100 : 0}%` } as CSSProperties;
  const volumeStyle = { '--range-progress': `${muted ? 0 : volume * 100}%` } as CSSProperties;
  const playbackStatus = error ? 'Błąd nagrania' : buffering ? 'Buforowanie…'
    : playing ? 'Odtwarzanie' : !ready ? 'Wczytywanie nagrania…' : currentTime > 0 ? 'Wstrzymano' : 'Gotowe do odsłuchu';

  return (
    <div className="lyrics-app">
      <audio ref={audioRef} src={`${MEDIA_BASE}rick.mp3`} preload="metadata" />

      <header className="topbar">
        <a className="brand" href={MEDIA_BASE} aria-label="Lyrics — strona główna">lyrics<span>.</span></a>
        <span className="view-label">TEKST NAGRANIA</span>
      </header>

      <main className="stage">
        <section className="track-heading" aria-labelledby="track-title">
          <h1 id="track-title">rick</h1>
          <span className="track-meta">English <span aria-label="Treści niecenzuralne" title="Treści niecenzuralne" className="explicit-badge">E</span></span>
        </section>

        <div className="lyrics-area">
          <div
            className="lyrics-scroll"
            ref={scrollerRef}
            tabIndex={0}
            role="region"
            aria-label="Tekst nagrania — kliknij wers, aby przewinąć audio"
            onWheel={() => setFollowing(false)}
            onTouchMove={() => setFollowing(false)}
            onKeyDown={handleScrollerKey}
            onPointerDown={event => {
              if (event.target === event.currentTarget) setFollowing(false);
            }}
          >
            {transcriptState.status === 'loading' && (
              <div className="empty-state" role="status"><span className="loading-orbit" /><p>Wczytywanie tekstu nagrania…</p></div>
            )}
            {transcriptState.status === 'error' && (
              <div className="empty-state" role="alert">
                <h2>Nie udało się wczytać tekstu.</h2>
                <p>{transcriptState.message}</p>
                <button className="pill-button" onClick={() => setLoadAttempt(value => value + 1)}><Icon name="retry" /> Spróbuj ponownie</button>
              </div>
            )}
            {transcriptState.status === 'ready' && !hasWords && (
              <div className="empty-state"><h2>Tu gra sam dźwięk.</h2><p>To nagranie nie ma jeszcze tekstu. Nadal możesz go posłuchać.</p></div>
            )}
            {hasWords && (
              <div className="lyric-lines">
                {lines.map((line, index) => (
                  <LyricRow
                    key={line.id}
                    line={line}
                    currentTime={currentTime < line.start ? -1 : Math.min(currentTime, line.end)}
                    focused={index === focusedIndex}
                    past={index < focusedIndex}
                    onSeek={seekToLine}
                  />
                ))}
                <p className="lyrics-end">Koniec tekstu</p>
              </div>
            )}
          </div>
          {!following && hasWords && (
            <button className="follow-button pill-button" onClick={() => { setFollowing(true); centerLine(); }}><Icon name="follow" /> Wróć do tekstu</button>
          )}
        </div>
      </main>

      <footer className="player" aria-label="Odtwarzacz nagrania">
        {error && <div className="audio-error" role="alert"><span>{error}</span><button onClick={audio.retry}><Icon name="retry" /> Ponów</button></div>}
        <div className="player-inner">
          <div className="player-track"><span className="player-track-title">rick</span><span className="playback-status" role="status">{playbackStatus}</span></div>
          <div className="transport-buttons">
            <button className="icon-button skip-button" onClick={() => audio.skip(-SEEK_STEP_SECONDS)} disabled={!canSeek} aria-label="Cofnij o 5 sekund" title="Cofnij o 5 sekund"><Icon name="back" /></button>
            <button className="play-button" onClick={() => void audio.togglePlayback()} disabled={!!error} aria-label={playing ? 'Wstrzymaj' : 'Odtwórz'} title={playing ? 'Wstrzymaj' : 'Odtwórz'}><Icon name={playing ? 'pause' : 'play'} /></button>
            <button className="icon-button skip-button" onClick={() => audio.skip(SEEK_STEP_SECONDS)} disabled={!canSeek} aria-label="Przewiń o 5 sekund" title="Przewiń o 5 sekund"><Icon name="forward" /></button>
          </div>
          <div className="seek-bar">
            <span className="time-label">{formatTime(currentTime)}</span>
            <input
              aria-label="Pozycja odtwarzania"
              aria-valuetext={duration === null ? `${formatTime(currentTime)}, czas nagrania nieznany` : `${formatTime(currentTime)} z ${durationLabel}`}
              type="range" min="0" max={duration ?? 0} step="0.01"
              value={duration === null ? 0 : Math.min(currentTime, duration)}
              disabled={!canSeek} style={progressStyle}
              onChange={event => seek(Number(event.target.value))}
            />
            <span className="time-label">{durationLabel}</span>
          </div>
          <div className="volume-control">
            <button className="icon-button" onClick={audio.toggleMute} aria-label={muted || volume === 0 ? 'Włącz dźwięk' : 'Wycisz'} aria-pressed={muted || volume === 0} title={muted || volume === 0 ? 'Włącz dźwięk' : 'Wycisz'}><Icon name={muted || volume === 0 ? 'muted' : 'volume'} /></button>
            <input aria-label="Głośność" aria-valuetext={`${Math.round(volume * 100)}%`} type="range" min="0" max="1" step="0.01" value={volume} style={volumeStyle} onChange={event => audio.setVolume(Number(event.target.value))} />
          </div>
        </div>
      </footer>
    </div>
  );
}
