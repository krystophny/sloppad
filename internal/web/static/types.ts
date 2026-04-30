export interface Label {
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
  labels?: Label[];
}

export interface Artifact {
  id: number;
  kind: string;
  title: string;
  ref_path?: string;
  ref_url?: string;
  labels?: Label[];
}

export interface Item {
  id: number;
  title: string;
  kind?: 'action' | 'project';
  state: 'inbox' | 'waiting' | 'someday' | 'done';
  workspace_id?: number | null;
  artifact_id?: number | null;
  actor_id?: number | null;
  labels?: Label[];
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
