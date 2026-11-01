type IconName = 'play' | 'pause' | 'back' | 'forward' | 'volume' | 'muted' | 'follow' | 'arrow' | 'retry' | 'headphones';

export default function Icon({ name, className = '' }: { name: IconName; className?: string }) {
  const paths: Record<IconName, React.ReactNode> = {
    play: <path d="m9 5 11 7-11 7Z" fill="currentColor" stroke="none" />,
    pause: <><path d="M8 5v14M16 5v14" strokeWidth="4" /></>,
    back: <path d="m11 6-6 6 6 6m8-12-6 6 6 6" />,
    forward: <path d="m5 6 6 6-6 6m8-12 6 6-6 6" />,
    volume: <><path d="m11 4-6 5H2v6h3l6 5Z" /><path d="M15 8a6 6 0 0 1 0 8m3-11a10 10 0 0 1 0 14" /></>,
    muted: <><path d="m11 4-6 5H2v6h3l6 5Z" /><path d="m16 9 6 6m0-6-6 6" /></>,
    follow: <><path d="M9 3H3v6m12-6h6v6M3 15v6h6m12-6v6h-6" /><path d="m9 12 2 2 4-4" /></>,
    arrow: <path d="m8 5 7 7-7 7" />,
    retry: <><path d="M20 8a9 9 0 1 0 1 7M20 3v5h-5" /></>,
    headphones: <><path d="M4 14v-2a8 8 0 0 1 16 0v2" /><rect x="3" y="12" width="4" height="8" rx="2" /><rect x="17" y="12" width="4" height="8" rx="2" /></>,
  };
  return <svg className={className} viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.7" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">{paths[name]}</svg>;
}
