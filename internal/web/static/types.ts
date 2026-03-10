export interface Context {
  id: number;
  name: string;
  color?: string;
  parent_id?: number | null;
}

export interface Workspace {
  id: number;
  name: string;
  dir_path: string;
  sphere?: 'work' | 'private' | '';
  is_active: boolean;
  contexts?: Context[];
}

export interface Artifact {
  id: number;
  kind: string;
  title: string;
  ref_path?: string;
  ref_url?: string;
  contexts?: Context[];
}

export interface Item {
  id: number;
  title: string;
  state: 'inbox' | 'waiting' | 'someday' | 'done';
  workspace_id?: number | null;
  artifact_id?: number | null;
  actor_id?: number | null;
  contexts?: Context[];
}

export interface Actor {
  id: number;
  name: string;
  kind: 'human' | 'agent';
}

export interface WebSocketEnvelope {
  type: string;
  [key: string]: unknown;
}
