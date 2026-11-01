export interface MockTrack {
  readonly id: string;
  readonly title: string;
  readonly audioPath: string;
  readonly transcriptPath: string;
}

export const MOCK_TRACKS = [
  {
    id: 'rick',
    title: 'rick',
    audioPath: 'rick/rick.mp3',
    transcriptPath: 'rick/transcript.json',
  },
  {
    id: 'lovemenot',
    title: 'lovemenot',
    audioPath: 'lovemenot/lovemenot.mp3',
    transcriptPath: 'lovemenot/transcript.json',
  },
] as const satisfies readonly MockTrack[];

export const DEFAULT_TRACK = MOCK_TRACKS[0];
