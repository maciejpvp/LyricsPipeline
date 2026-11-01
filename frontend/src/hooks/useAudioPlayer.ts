import { useCallback, useEffect, useRef, useState, type RefObject } from 'react';

const DEFAULT_VOLUME = 0.75;

function finiteDuration(audio: HTMLAudioElement): number | null {
  return Number.isFinite(audio.duration) && audio.duration > 0 ? audio.duration : null;
}

/** Read the media element on mount as well as on events: metadata may precede hydration. */
export function useAudioPlayer(audioRef: RefObject<HTMLAudioElement | null>) {
  const [currentTime, setCurrentTime] = useState(0);
  const [duration, setDuration] = useState<number | null>(null);
  const [playing, setPlaying] = useState(false);
  const [ready, setReady] = useState(false);
  const [buffering, setBuffering] = useState(false);
  const [volume, setVolumeState] = useState(DEFAULT_VOLUME);
  const [muted, setMuted] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const playRequest = useRef(0);
  const starting = useRef(false);

  const syncMedia = useCallback(() => {
    const audio = audioRef.current;
    if (!audio) return;
    setCurrentTime(Number.isFinite(audio.currentTime) ? audio.currentTime : 0);
    setDuration(finiteDuration(audio));
    setPlaying(!audio.paused && !audio.ended);
    setReady(audio.readyState >= HTMLMediaElement.HAVE_METADATA || finiteDuration(audio) !== null);
    setVolumeState(audio.volume);
    setMuted(audio.muted);
  }, [audioRef]);

  useEffect(() => {
    const audio = audioRef.current;
    if (!audio) return;
    audio.volume = DEFAULT_VOLUME;

    const update = () => syncMedia();
    const waiting = () => { setBuffering(true); syncMedia(); };
    const settled = () => { setBuffering(false); syncMedia(); };
    const failed = () => {
      starting.current = false;
      playRequest.current += 1;
      setBuffering(false);
      setError('Nie udało się wczytać rick.mp3. Sprawdź plik nagrania i spróbuj ponownie.');
      syncMedia();
      setPlaying(false);
    };
    const events = ['loadedmetadata', 'durationchange', 'loadeddata', 'timeupdate', 'seeking', 'seeked', 'volumechange'];
    const settledEvents = ['canplay', 'play', 'playing', 'pause', 'ended', 'emptied'];
    events.forEach(name => audio.addEventListener(name, update));
    settledEvents.forEach(name => audio.addEventListener(name, settled));
    audio.addEventListener('waiting', waiting);
    audio.addEventListener('error', failed);

    // A cached file can already have its metadata by the time this effect runs.
    syncMedia();
    if (audio.error) failed();

    return () => {
      playRequest.current += 1;
      events.forEach(name => audio.removeEventListener(name, update));
      settledEvents.forEach(name => audio.removeEventListener(name, settled));
      audio.removeEventListener('waiting', waiting);
      audio.removeEventListener('error', failed);
    };
  }, [audioRef, syncMedia]);

  useEffect(() => {
    if (!playing) return;
    let frame: number;
    const tick = () => {
      const audio = audioRef.current;
      if (audio) setCurrentTime(audio.currentTime);
      frame = requestAnimationFrame(tick);
    };
    frame = requestAnimationFrame(tick);
    return () => cancelAnimationFrame(frame);
  }, [audioRef, playing]);

  const seek = useCallback((time: number) => {
    const audio = audioRef.current;
    if (!audio || !Number.isFinite(time)) return;
    const availableDuration = finiteDuration(audio);
    if (availableDuration === null) return;
    audio.currentTime = Math.max(0, Math.min(time, availableDuration));
    syncMedia();
  }, [audioRef, syncMedia]);

  const skip = useCallback((seconds: number) => {
    seek((audioRef.current?.currentTime ?? 0) + seconds);
  }, [audioRef, seek]);

  const togglePlayback = useCallback(async () => {
    const audio = audioRef.current;
    if (!audio) return;
    const request = ++playRequest.current;
    if (starting.current || !audio.paused) {
      starting.current = false;
      audio.pause();
      setBuffering(false);
      syncMedia();
      return;
    }
    starting.current = true;
    setError(null);
    try {
      if (audio.ended) seek(0);
      await audio.play();
      if (request === playRequest.current) syncMedia();
    } catch (cause: unknown) {
      // Pausing or reloading can intentionally interrupt an outstanding play().
      if (request === playRequest.current && !(cause instanceof DOMException && cause.name === 'AbortError')) {
        setError('Nie udało się odtworzyć nagrania. Spróbuj ponownie.');
      }
    } finally {
      if (request === playRequest.current) starting.current = false;
    }
  }, [audioRef, seek, syncMedia]);

  const retry = useCallback(() => {
    playRequest.current += 1;
    starting.current = false;
    setError(null);
    setBuffering(false);
    setCurrentTime(0);
    setDuration(null);
    setPlaying(false);
    setReady(false);
    audioRef.current?.load();
  }, [audioRef]);

  const setVolume = useCallback((value: number) => {
    const audio = audioRef.current;
    if (!audio) return;
    audio.volume = Math.max(0, Math.min(1, value));
    audio.muted = false;
    syncMedia();
  }, [audioRef, syncMedia]);

  const toggleMute = useCallback(() => {
    const audio = audioRef.current;
    if (!audio) return;
    if (audio.volume === 0) {
      audio.volume = DEFAULT_VOLUME;
      audio.muted = false;
    } else {
      audio.muted = !audio.muted;
    }
    syncMedia();
  }, [audioRef, syncMedia]);

  return { currentTime, duration, playing, ready, buffering, volume, muted, error, seek, skip, togglePlayback, toggleMute, setVolume, retry };
}
