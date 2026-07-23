import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import type { FormEvent, ReactNode } from 'react';
import './style.css';
import vpsboxIcon from './assets/images/vpsbox-icon.svg';
import {
  CheckForUpdate,
  GetServerLogs,
  GetState,
  OpenExternal,
  OpenShell,
  ReadSSHKeys,
  RevealKeyFolder,
  StartCreateSandbox,
  StartDestroySandbox,
  StartFixLocalDomains,
  StartGenerateSSHKey,
  StartInstallPackages,
  StartStartSandbox,
  StartStopSandbox,
  StartUpdateSandbox,
} from '../wailsjs/go/main/DesktopApp';

// ============================================================================
// Types — mirror the Go DesktopApp shapes; do not change without updating Go.
// ============================================================================

type Requirement = {
  name: string;
  status: string;
  details: string;
  installed: boolean;
  description: string;
};

type Sandbox = {
  name: string;
  status: string;
  host: string;
  hostname: string;
  username: string;
  privateKeyPath: string;
  hasPrivateKey: boolean;
  backend: string;
  createdAt: string;
  cpus: number;
  memoryGB: number;
  diskGB: number;
  imported: boolean;
};

type Job = {
  id: string;
  kind: string;
  target: string;
  state: string;
  message: string;
  startedAt: string;
  finishedAt?: string;
};

type UpdateInfo = {
  available: boolean;
  current: string;
  latest: string;
  url: string;
  checkedAt?: string;
  releasedAt?: string;
  error?: string;
};

type ServerLogEntry = {
  id: string;
  category: 'system' | 'network' | 'route' | 'docker';
  timestamp?: string;
  level: string;
  source: string;
  message: string;
};

type ServerLogs = {
  fetchedAt: string;
  entries: ServerLogEntry[];
};

type AppState = {
  appVersion: string;
  platform: string;
  requirements: Requirement[];
  instances: Sandbox[];
  jobs: Job[];
  update?: UpdateInfo;
};

type Section = 'servers' | 'system' | 'activity';
type DetailTab = 'overview' | 'connect' | 'logs' | 'resources';
type LogCategory = 'all' | ServerLogEntry['category'];
type StatusVariant = 'running' | 'stopped' | 'pending' | 'error' | 'info';

type EditValues = {
  name: string;
  cpus: number;
  memoryGB: number;
  diskGB: number;
};

type CreateValues = {
  name: string;
  cpus: number;
  memoryGB: number;
  diskGB: number;
  selfSigned: boolean;
};

// ============================================================================
// Constants
// ============================================================================

const initialState: AppState = {
  appVersion: '',
  platform: '',
  requirements: [],
  instances: [],
  jobs: [],
};

const PACKAGES = ['multipass', 'mkcert', 'cloudflared'] as const;

const PACKAGE_TITLES: Record<string, string> = {
  multipass: 'Multipass',
  mkcert: 'mkcert',
  cloudflared: 'cloudflared',
};

const CREATE_STAGES = [
  { id: 'check', title: 'Check VM backend', description: 'Verify Multipass is ready.' },
  { id: 'ssh', title: 'Generate SSH key', description: 'Create the sandbox SSH key.' },
  { id: 'bootstrap', title: 'Cloud-init bootstrap', description: 'Prepare the first-boot script.' },
  { id: 'launch', title: 'Launch Ubuntu VM', description: 'Start the Ubuntu instance.' },
  { id: 'wait', title: 'Wait for boot', description: 'Wait for cloud-init to finish.' },
  { id: 'finalize', title: 'Network and TLS', description: 'Set local domains and certificates.' },
  { id: 'ready', title: 'Register sandbox', description: 'Save the registry entry.' },
] as const;

const DEFAULT_CREATE: CreateValues = {
  name: '',
  cpus: 2,
  memoryGB: 2,
  diskGB: 10,
  selfSigned: false,
};

// macOS hides the title bar and floats the traffic lights over our toolbar, so
// the lead cell needs an inset there and nowhere else.
const IS_MAC = /Mac/i.test(navigator.platform || navigator.userAgent);

const ZOOM_KEY = 'vpsbox.zoom';
const ZOOM_MIN = 0.8;
const ZOOM_MAX = 1.6;
const ZOOM_STEP = 0.1;

// ============================================================================
// Pure helpers
// ============================================================================

function toMessage(error: unknown): string {
  if (error instanceof Error) return error.message;
  if (typeof error === 'string') return error;
  return 'Something went wrong.';
}

function formatRelative(iso: string): string {
  if (!iso) return '';
  const then = Date.parse(iso);
  if (Number.isNaN(then)) return iso;
  const seconds = Math.max(0, Math.round((Date.now() - then) / 1000));
  if (seconds < 5) return 'just now';
  if (seconds < 60) return `${seconds}s ago`;
  if (seconds < 3600) return `${Math.round(seconds / 60)}m ago`;
  if (seconds < 86400) return `${Math.round(seconds / 3600)}h ago`;
  return `${Math.round(seconds / 86400)}d ago`;
}

function formatClockTime(iso: string): string {
  if (!iso) return 'Now';
  const value = new Date(iso);
  if (Number.isNaN(value.getTime())) return iso;
  return value.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit', second: '2-digit' });
}

function statusVariant(status: string): StatusVariant {
  switch (status.toLowerCase()) {
    case 'running':
    case 'done':
    case 'ok':
    case 'up to date':
      return 'running';
    case 'stopped':
      return 'stopped';
    case 'error':
    case 'fail':
      return 'error';
    case 'pending':
    case 'starting':
    case 'update available':
      return 'pending';
    default:
      return 'info';
  }
}

function shellQuote(value: string): string {
  return `'${value.replace(/'/g, `'\\''`)}'`;
}

function connectionHost(instance: Sandbox): string {
  return instance.host || instance.hostname || `${instance.name}.vpsbox.local`;
}

function buildQuickCommands(instance: Sandbox) {
  const host = connectionHost(instance);
  const key = shellQuote(instance.privateKeyPath || `~/.vpsbox/keys/${instance.name}`);
  const target = `${instance.username || 'root'}@${host}`;
  const sample = '~/sample-app';
  return [
    {
      label: 'SSH into the sandbox',
      command: `ssh -i ${key} -o StrictHostKeyChecking=no ${target}`,
    },
    {
      label: 'Create a sample folder',
      command: `ssh -i ${key} -o StrictHostKeyChecking=no ${target} "mkdir -p ${sample}"`,
    },
    {
      label: 'Copy a file to the sandbox',
      command: `scp -i ${key} ./local-file.txt ${target}:${sample}/`,
    },
    {
      label: 'Copy a folder to the sandbox',
      command: `scp -i ${key} -r ./local-folder ${target}:${sample}/`,
    },
    {
      label: 'Pull a file back to your machine',
      command: `scp -i ${key} ${target}:${sample}/file.txt ~/Downloads/`,
    },
  ];
}

function packageStatus(
  name: string,
  requirement: Requirement | undefined,
  installJob: Job | undefined,
): { variant: StatusVariant; label: string; details: string } {
  if (requirement?.installed) {
    return { variant: 'running', label: 'Installed', details: requirement.details || 'Ready' };
  }
  if (installJob?.state === 'error') {
    return { variant: 'error', label: 'Failed', details: installJob.message };
  }
  if (installJob?.state === 'running') {
    const message = installJob.message.toLowerCase();
    const matches = message.includes(name) || (name === 'mkcert' && message.includes('certificate'));
    if (matches) {
      return { variant: 'pending', label: 'Installing', details: installJob.message };
    }
  }
  return {
    variant: 'stopped',
    label: 'Not installed',
    details: requirement?.details || 'Waiting',
  };
}

function createStageIndex(message: string, state: string): number {
  if (state === 'done') return CREATE_STAGES.length - 1;
  const m = message.toLowerCase();
  if (m.includes('generating ssh key')) return 1;
  if (m.includes('preparing ubuntu bootstrap script')) return 2;
  if (m.includes('launching ubuntu vm') || m.includes('starting existing sandbox')) return 3;
  if (m.includes('waiting for ubuntu initialization')) return 4;
  if (
    m.includes('finalizing local networking and certificates') ||
    m.includes('writing local registry')
  ) {
    return 5;
  }
  if (m.includes('sandbox is ready')) return 6;
  return 0;
}

type StageState = 'done' | 'active' | 'error' | 'idle';

function stageStateFor(stageIndex: number, job: Job | undefined): StageState {
  if (!job) return 'idle';
  const current = createStageIndex(job.message, job.state);
  if (job.state === 'error') {
    if (stageIndex < current) return 'done';
    if (stageIndex === current) return 'error';
    return 'idle';
  }
  if (job.state === 'done') return 'done';
  if (stageIndex < current) return 'done';
  if (stageIndex === current) return 'active';
  return 'idle';
}

function jobLabel(kind: string): string {
  switch (kind) {
    case 'install':
      return 'Installing packages';
    case 'create':
      return 'Creating server';
    case 'start':
      return 'Starting server';
    case 'stop':
      return 'Stopping server';
    case 'destroy':
      return 'Deleting server';
    case 'update':
      return 'Updating server';
    case 'sshkey':
      return 'Generating SSH key';
    case 'domains':
      return 'Updating /etc/hosts';
    case 'bootstrap':
      return 'Backend bootstrap';
    default:
      return kind;
  }
}

function clampZoom(value: number): number {
  // Round to the step so repeated arithmetic can't drift off the scale.
  const stepped = Math.round(value / ZOOM_STEP) * ZOOM_STEP;
  return Math.min(ZOOM_MAX, Math.max(ZOOM_MIN, Number(stepped.toFixed(2))));
}

function readStoredZoom(): number {
  try {
    const raw = window.localStorage.getItem(ZOOM_KEY);
    if (!raw) return 1;
    const parsed = Number.parseFloat(raw);
    return Number.isFinite(parsed) ? clampZoom(parsed) : 1;
  } catch {
    return 1;
  }
}

async function copyToClipboard(text: string): Promise<boolean> {
  try {
    await navigator.clipboard.writeText(text);
    return true;
  } catch {
    return false;
  }
}

// ============================================================================
// Icons — 16px stroke set, sized to sit on a 26px control row
// ============================================================================

type IconName =
  | 'server'
  | 'gear'
  | 'pulse'
  | 'plus'
  | 'terminal'
  | 'play'
  | 'stop'
  | 'key'
  | 'folder'
  | 'refresh'
  | 'download'
  | 'external'
  | 'close'
  | 'check'
  | 'sliders'
  | 'inbox'
  | 'globe';

const ICON_PATHS: Record<IconName, ReactNode> = {
  server: (
    <>
      <rect x="2.5" y="3" width="11" height="4.2" rx="1.3" />
      <rect x="2.5" y="8.8" width="11" height="4.2" rx="1.3" />
      <path d="M5 5.1h.01M5 10.9h.01" />
    </>
  ),
  gear: (
    <>
      <circle cx="8" cy="8" r="2" />
      <path d="M12.7 9.7a1.1 1.1 0 0 0 .2 1.2l.1.1a1.3 1.3 0 1 1-1.9 1.9l-.1-.1a1.1 1.1 0 0 0-1.2-.2 1.1 1.1 0 0 0-.7 1v.2a1.3 1.3 0 1 1-2.6 0v-.1a1.1 1.1 0 0 0-.7-1 1.1 1.1 0 0 0-1.2.2l-.1.1a1.3 1.3 0 1 1-1.9-1.9l.1-.1a1.1 1.1 0 0 0 .2-1.2 1.1 1.1 0 0 0-1-.7h-.2a1.3 1.3 0 1 1 0-2.6h.1a1.1 1.1 0 0 0 1-.7 1.1 1.1 0 0 0-.2-1.2l-.1-.1a1.3 1.3 0 1 1 1.9-1.9l.1.1a1.1 1.1 0 0 0 1.2.2h.1a1.1 1.1 0 0 0 .7-1v-.2a1.3 1.3 0 1 1 2.6 0v.1a1.1 1.1 0 0 0 .7 1 1.1 1.1 0 0 0 1.2-.2l.1-.1a1.3 1.3 0 1 1 1.9 1.9l-.1.1a1.1 1.1 0 0 0-.2 1.2v.1a1.1 1.1 0 0 0 1 .7h.2a1.3 1.3 0 1 1 0 2.6h-.1a1.1 1.1 0 0 0-1 .7z" />
    </>
  ),
  pulse: <path d="M1.8 8h3L6.4 4.5 9.2 11.5l1.5-3.5h3.5" />,
  plus: <path d="M8 3.4v9.2M3.4 8h9.2" />,
  terminal: (
    <>
      <rect x="2" y="2.8" width="12" height="10.4" rx="1.6" />
      <path d="M4.8 6.4 6.9 8.4l-2.1 2M8.8 11h2.6" />
    </>
  ),
  play: <path d="M5.4 3.6 12 8l-6.6 4.4z" />,
  stop: <rect x="4.4" y="4.4" width="7.2" height="7.2" rx="1.4" />,
  key: (
    <>
      <circle cx="10.3" cy="5.7" r="2.6" />
      <path d="M8.5 7.5 2.8 13.2M4.4 11.6l1.5 1.5M5.9 10.1l1.5 1.5" />
    </>
  ),
  folder: (
    <path d="M2.2 5.4c0-.8.6-1.4 1.3-1.4h2.1l1.3 1.6h4.6c.7 0 1.3.6 1.3 1.4v4.4c0 .8-.6 1.4-1.3 1.4H3.5c-.7 0-1.3-.6-1.3-1.4z" />
  ),
  refresh: (
    <>
      <path d="M13.2 8a5.2 5.2 0 1 1-1.6-3.7" />
      <path d="M13.4 2.6v3.2h-3.2" />
    </>
  ),
  download: <path d="M8 2.6v7.6M4.9 7.4 8 10.4l3.1-3M2.8 12.8h10.4" />,
  external: (
    <>
      <path d="M9.6 2.8h3.6v3.6" />
      <path d="M13.2 2.8 7.6 8.4" />
      <path d="M12.2 9.6v2.8c0 .7-.5 1.2-1.2 1.2H3.8c-.7 0-1.2-.5-1.2-1.2V5.2c0-.7.5-1.2 1.2-1.2h2.8" />
    </>
  ),
  close: <path d="M4.2 4.2l7.6 7.6M11.8 4.2l-7.6 7.6" />,
  check: <path d="M3.2 8.4 6.4 11.6l6.4-7.2" />,
  sliders: <path d="M3 4.6h10M3 11.4h10M6.4 2.9v3.4M10.2 9.7v3.4" />,
  inbox: (
    <>
      <path d="M2.2 8.6h3.2l1 2h3.2l1-2h3.2" />
      <path d="M4.1 3.2h7.8l1.9 5.4v3.6c0 .7-.5 1.2-1.2 1.2H3.4c-.7 0-1.2-.5-1.2-1.2V8.6z" />
    </>
  ),
  globe: (
    <>
      <circle cx="8" cy="8" r="5.6" />
      <path d="M2.6 6.2h10.8M2.6 9.8h10.8" />
      <path d="M8 2.4c1.6 1.6 2.4 3.5 2.4 5.6S9.6 12 8 13.6C6.4 12 5.6 10.1 5.6 8s.8-4 2.4-5.6z" />
    </>
  ),
};

function Icon({ name, size = 16 }: { name: IconName; size?: number }) {
  const filled = name === 'play' || name === 'stop';
  return (
    <svg
      width={size}
      height={size}
      viewBox="0 0 16 16"
      fill={filled ? 'currentColor' : 'none'}
      stroke="currentColor"
      strokeWidth={1.4}
      strokeLinecap="round"
      strokeLinejoin="round"
      aria-hidden
      focusable="false"
    >
      {ICON_PATHS[name]}
    </svg>
  );
}

// ============================================================================
// Primitive components
// ============================================================================

function StatusChip({ status, label }: { status: string; label?: string }) {
  const variant = statusVariant(status);
  // Go hands us lowercase states ("running"); render them in sentence case.
  const text = label ?? (status ? status[0].toUpperCase() + status.slice(1) : 'Unknown');
  return (
    <span className={`chip chip-${variant}`}>
      <span className={`led led-${variant}`} aria-hidden />
      {text}
    </span>
  );
}

type ButtonProps = {
  children: ReactNode;
  onClick?: () => void;
  variant?: 'primary' | 'ghost' | 'danger';
  busy?: boolean;
  disabled?: boolean;
  size?: 'sm' | 'md';
  type?: 'button' | 'submit';
  title?: string;
  icon?: IconName;
};

function Button({
  children,
  onClick,
  variant = 'ghost',
  busy = false,
  disabled = false,
  size = 'sm',
  type = 'button',
  title,
  icon,
}: ButtonProps) {
  return (
    <button
      type={type}
      className={`btn ${variant === 'ghost' ? '' : `btn-${variant}`} ${size === 'md' ? 'btn-lg' : ''}`}
      onClick={onClick}
      disabled={disabled || busy}
      aria-busy={busy || undefined}
      title={title}
    >
      {busy ? <span className="btn-spin" aria-hidden /> : icon ? <Icon name={icon} size={13} /> : null}
      <span>{children}</span>
    </button>
  );
}

function ToolbarButton({
  children,
  onClick,
  icon,
  accent = false,
  disabled = false,
  title,
  label,
}: {
  children?: ReactNode;
  onClick: () => void;
  icon: IconName;
  accent?: boolean;
  disabled?: boolean;
  title?: string;
  label?: string;
}) {
  return (
    <button
      type="button"
      className={`tbtn ${children ? '' : 'tbtn-icon'} ${accent ? 'tbtn-accent' : ''}`}
      onClick={onClick}
      disabled={disabled}
      title={title}
      aria-label={label ?? title}
    >
      <Icon name={icon} />
      {children ? <span>{children}</span> : null}
    </button>
  );
}

function Blank({
  icon = 'inbox',
  title,
  description,
  action,
}: {
  icon?: IconName;
  title: string;
  description: string;
  action?: ReactNode;
}) {
  return (
    <div className="blank">
      <Icon name={icon} size={26} />
      <strong>{title}</strong>
      <p>{description}</p>
      {action ? <div className="blank-act">{action}</div> : null}
    </div>
  );
}

function Row({ label, children }: { label: string; children: ReactNode }) {
  return (
    <>
      <dt>{label}</dt>
      <dd>{children}</dd>
    </>
  );
}

function Inspector({ title, action, children }: { title: string; action?: ReactNode; children: ReactNode }) {
  return (
    <section className="inspector">
      <header className="inspector-head">
        <span>{title}</span>
        {action}
      </header>
      <dl className="kv">{children}</dl>
    </section>
  );
}

function Segments<T extends string>({
  value,
  options,
  onChange,
  label,
}: {
  value: T;
  options: { id: T; label: string }[];
  onChange: (id: T) => void;
  label: string;
}) {
  return (
    <div className="segments" role="tablist" aria-label={label}>
      {options.map((option) => (
        <button
          key={option.id}
          type="button"
          role="tab"
          aria-selected={value === option.id}
          className={`segment ${value === option.id ? 'segment-on' : ''}`}
          onClick={() => onChange(option.id)}
        >
          {option.label}
        </button>
      ))}
    </div>
  );
}

function Zoomer({ zoom, onChange }: { zoom: number; onChange: (next: number) => void }) {
  const shortcut = IS_MAC ? '⌘' : 'Ctrl+';
  return (
    <div className="zoomer">
      <button
        type="button"
        onClick={() => onChange(zoom - ZOOM_STEP)}
        disabled={zoom <= ZOOM_MIN}
        title={`Zoom out (${shortcut}−)`}
        aria-label="Zoom out"
      >
        −
      </button>
      <output
        onClick={() => onChange(1)}
        title={`Reset to 100% (${shortcut}0)`}
        aria-live="polite"
      >
        {Math.round(zoom * 100)}%
      </output>
      <button
        type="button"
        onClick={() => onChange(zoom + ZOOM_STEP)}
        disabled={zoom >= ZOOM_MAX}
        title={`Zoom in (${shortcut}+)`}
        aria-label="Zoom in"
      >
        +
      </button>
    </div>
  );
}

function Meter({ done, total }: { done: number; total: number }) {
  const percent = total === 0 ? 0 : Math.round((done / total) * 100);
  return (
    <div className="meter">
      <div className="meter-track">
        <div
          className={`meter-fill ${done === total ? 'meter-fill-done' : ''}`}
          style={{ width: `${percent}%` }}
        />
      </div>
      <span>
        {done} / {total} ready
      </span>
    </div>
  );
}

// ============================================================================
// Main app
// ============================================================================

function App() {
  const [state, setState] = useState<AppState>(initialState);
  const [bootstrapped, setBootstrapped] = useState(false);
  const [error, setError] = useState('');
  const [section, setSection] = useState<Section>('servers');
  const [selectedName, setSelectedName] = useState<string | null>(null);
  const [detailTab, setDetailTab] = useState<DetailTab>('overview');
  const [createOpen, setCreateOpen] = useState(false);
  const [editing, setEditing] = useState<EditValues | null>(null);
  const [confirmingDelete, setConfirmingDelete] = useState<Sandbox | null>(null);
  const [actionsBusy, setActionsBusy] = useState<Record<string, boolean>>({});
  const [copiedCommand, setCopiedCommand] = useState('');
  const [viewingKeys, setViewingKeys] = useState<{ privateKey: string; publicKey: string } | null>(null);
  const [updateDismissed, setUpdateDismissed] = useState(false);
  const [zoom, setZoom] = useState<number>(readStoredZoom);

  const requirementsByName = useMemo(() => {
    const map = new Map<string, Requirement>();
    for (const req of state.requirements) map.set(req.name, req);
    return map;
  }, [state.requirements]);

  const hasCorePackages = useMemo(
    () => PACKAGES.every((name) => requirementsByName.get(name)?.installed),
    [requirementsByName],
  );

  const installJob = useMemo(
    () => state.jobs.find((job) => job.kind === 'install'),
    [state.jobs],
  );

  const createJob = useMemo(
    () => state.jobs.find((job) => job.kind === 'create' || job.kind === 'start'),
    [state.jobs],
  );

  const activeJobs = useMemo(
    () => state.jobs.filter((job) => job.state === 'running'),
    [state.jobs],
  );

  const selectedInstance = useMemo(
    () => state.instances.find((instance) => instance.name === selectedName) ?? null,
    [selectedName, state.instances],
  );

  const runningCount = useMemo(
    () => state.instances.filter((i) => i.status.toLowerCase() === 'running').length,
    [state.instances],
  );

  // Self-scheduling poll loop. Cadence depends on whether jobs are running,
  // but we read that through a ref so we don't tear down the loop on every change.
  const activeCountRef = useRef(0);
  useEffect(() => {
    activeCountRef.current = activeJobs.length;
  }, [activeJobs.length]);

  useEffect(() => {
    let cancelled = false;
    let timer: number | undefined;

    const fetchOnce = async () => {
      try {
        const next = await GetState();
        if (cancelled) return;
        setState(next as unknown as AppState);
        setBootstrapped(true);
      } catch (err) {
        if (!cancelled) setError(toMessage(err));
      } finally {
        if (!cancelled) {
          const delay = activeCountRef.current > 0 ? 1500 : 4000;
          timer = window.setTimeout(fetchOnce, delay);
        }
      }
    };

    void fetchOnce();
    return () => {
      cancelled = true;
      if (timer != null) window.clearTimeout(timer);
    };
  }, []);

  // When a create job finishes, close the sheet and select the new server.
  const lastCreateState = useRef<string | undefined>(undefined);
  useEffect(() => {
    const previous = lastCreateState.current;
    lastCreateState.current = createJob?.state;
    if (
      createOpen &&
      createJob?.state === 'done' &&
      previous !== 'done' &&
      state.instances.some((i) => i.name === createJob.target)
    ) {
      setCreateOpen(false);
      setSelectedName(createJob.target);
      setSection('servers');
      setDetailTab('overview');
    }
  }, [createJob, createOpen, state.instances]);

  // Auto-select the first instance if nothing is selected.
  useEffect(() => {
    if (selectedName == null && state.instances.length > 0) {
      setSelectedName(state.instances[0].name);
    }
  }, [selectedName, state.instances]);

  // If the selected instance disappears (e.g. destroyed), pick the next one.
  useEffect(() => {
    if (selectedName != null && !state.instances.some((i) => i.name === selectedName)) {
      setSelectedName(state.instances[0]?.name ?? null);
    }
  }, [selectedName, state.instances]);

  const runAction = useCallback(async (key: string, fn: () => Promise<unknown>) => {
    setActionsBusy((prev) => ({ ...prev, [key]: true }));
    setError('');
    try {
      await fn();
      const next = await GetState();
      setState(next as unknown as AppState);
    } catch (err) {
      setError(toMessage(err));
    } finally {
      setActionsBusy((prev) => {
        const next = { ...prev };
        delete next[key];
        return next;
      });
    }
  }, []);

  const changeZoom = useCallback((next: number) => {
    setZoom((current) => {
      const value = clampZoom(next);
      return value === current ? current : value;
    });
  }, []);

  // Drive the root scale factor and remember it across launches.
  useEffect(() => {
    document.documentElement.style.setProperty('--zoom', String(zoom));
    try {
      window.localStorage.setItem(ZOOM_KEY, String(zoom));
    } catch {
      // Storage can be unavailable; zoom still applies for this session.
    }
  }, [zoom]);

  // The desktop shell has no View menu, so bind the usual zoom shortcuts here.
  useEffect(() => {
    const onKey = (event: KeyboardEvent) => {
      if (!event.metaKey && !event.ctrlKey) return;
      if (event.key === '=' || event.key === '+') {
        event.preventDefault();
        changeZoom(zoom + ZOOM_STEP);
      } else if (event.key === '-' || event.key === '_') {
        event.preventDefault();
        changeZoom(zoom - ZOOM_STEP);
      } else if (event.key === '0') {
        event.preventDefault();
        changeZoom(1);
      }
    };
    window.addEventListener('keydown', onKey);
    return () => window.removeEventListener('keydown', onKey);
  }, [changeZoom, zoom]);

  const handleCopy = useCallback(async (command: string) => {
    const ok = await copyToClipboard(command);
    if (ok) {
      setCopiedCommand(command);
      window.setTimeout(() => {
        setCopiedCommand((current) => (current === command ? '' : current));
      }, 1500);
    }
  }, []);

  const showFirstRun =
    bootstrapped &&
    section === 'servers' &&
    !hasCorePackages &&
    state.instances.length === 0;

  const crumb =
    section === 'system'
      ? { title: 'Host environment', sub: state.platform }
      : section === 'activity'
        ? { title: 'Activity', sub: `${state.jobs.length} job${state.jobs.length === 1 ? '' : 's'}` }
        : showFirstRun
          ? { title: 'Setup', sub: 'First run' }
          : selectedInstance
            ? {
                title: selectedInstance.name,
                sub: `${selectedInstance.username || 'root'}@${connectionHost(selectedInstance)}`,
              }
            : { title: 'Servers', sub: '' };

  return (
    <div className={`app ${IS_MAC ? 'app-mac' : ''}`}>
      <header className="toolbar">
        <div className="toolbar-lead">
          <img className="toolbar-mark" src={vpsboxIcon} alt="" aria-hidden />
          <span className="toolbar-word">vpsbox</span>
        </div>
        <div className="toolbar-main">
          <div className="crumb">
            <span className="crumb-title">{crumb.title}</span>
            {crumb.sub ? <span className="crumb-sub">{crumb.sub}</span> : null}
          </div>
          <div className="toolbar-actions">
            {section === 'servers' ? (
              <ToolbarButton
                icon="plus"
                accent
                onClick={() => setCreateOpen(true)}
                title="Create a new server"
              >
                New server
              </ToolbarButton>
            ) : null}
          </div>
        </div>
      </header>

      <div className="workspace">
        <aside className="sidebar">
          <div className="rail-group">
            <div className="rail-label">
              <span>Servers {state.instances.length > 0 ? state.instances.length : ''}</span>
              <button
                type="button"
                onClick={() => setCreateOpen(true)}
                title="Create a new server"
                aria-label="Create a new server"
              >
                <Icon name="plus" size={13} />
              </button>
            </div>
            <ul>
              {state.instances.length === 0 ? (
                <li>
                  <button
                    type="button"
                    className="rail-item rail-item-muted"
                    onClick={() => setCreateOpen(true)}
                  >
                    <Icon name="plus" size={14} />
                    <span className="rail-name">Create a server</span>
                  </button>
                </li>
              ) : null}
              {state.instances.map((instance) => (
                <li key={instance.name}>
                  <button
                    type="button"
                    className={`rail-item rail-item-vm ${
                      section === 'servers' && selectedName === instance.name ? 'rail-item-on' : ''
                    }`}
                    onClick={() => {
                      setSelectedName(instance.name);
                      setSection('servers');
                      setDetailTab('overview');
                    }}
                  >
                    <span className="rail-led" aria-hidden>
                      <span className={`led led-${statusVariant(instance.status)}`} />
                    </span>
                    <span className="rail-name">{instance.name}</span>
                  </button>
                </li>
              ))}
            </ul>
          </div>

          <div className="rail-group">
            <div className="rail-label">
              <span>Host</span>
            </div>
            <ul>
              <li>
                <button
                  type="button"
                  className={`rail-item ${section === 'system' ? 'rail-item-on' : ''}`}
                  onClick={() => setSection('system')}
                >
                  <Icon name="gear" size={14} />
                  <span className="rail-name">Environment</span>
                  {!hasCorePackages && bootstrapped ? (
                    <span className="rail-flag" title="Packages need install">
                      !
                    </span>
                  ) : null}
                </button>
              </li>
              <li>
                <button
                  type="button"
                  className={`rail-item ${section === 'activity' ? 'rail-item-on' : ''}`}
                  onClick={() => setSection('activity')}
                >
                  <Icon name="pulse" size={14} />
                  <span className="rail-name">Activity</span>
                  {activeJobs.length > 0 ? (
                    <span className="rail-tally rail-tally-live">{activeJobs.length}</span>
                  ) : null}
                </button>
              </li>
            </ul>
          </div>

          <div className="rail-spacer" />

          <button
            type="button"
            className="rail-promo"
            onClick={() => void OpenExternal('https://servercompass.app')}
            title="Open Server Compass"
          >
            <strong>Server Compass →</strong>
            <small>Real VPS fleets, when you're ready</small>
          </button>
        </aside>

        <main className="detail">
          {error ? (
            <div className="banner banner-error" role="alert">
              <div className="banner-body">
                <strong>Error</strong>
                <p>{error}</p>
              </div>
              <button type="button" className="banner-x" onClick={() => setError('')}>
                Dismiss
              </button>
            </div>
          ) : null}

          {state.update?.available && !updateDismissed ? (
            <div className="banner banner-update" role="status">
              <div className="banner-body">
                <strong>vpsbox {state.update.latest} is available</strong>
                <p>
                  You have {state.update.current}.{' '}
                  <button
                    type="button"
                    className="linkish"
                    onClick={() => void OpenExternal(state.update!.url)}
                  >
                    Download it
                  </button>
                </p>
              </div>
              <button type="button" className="banner-x" onClick={() => setUpdateDismissed(true)}>
                Dismiss
              </button>
            </div>
          ) : null}

          {!bootstrapped ? (
            <div className="boot">
              <div className="spin spin-lg" aria-hidden />
              <strong>Connecting to the local backend</strong>
              <p>Reading the sandbox registry and host requirements.</p>
            </div>
          ) : null}

          {showFirstRun ? (
            <SetupScreen
              requirements={state.requirements}
              installJob={installJob}
              installing={Boolean(actionsBusy.install)}
              hasCorePackages={hasCorePackages}
              onInstall={() => runAction('install', () => StartInstallPackages())}
              onCreate={() => setCreateOpen(true)}
            />
          ) : null}

          {bootstrapped && !showFirstRun && section === 'servers' ? (
            <ServersScreen
              instances={state.instances}
              selectedInstance={selectedInstance}
              detailTab={detailTab}
              setDetailTab={setDetailTab}
              actionsBusy={actionsBusy}
              runAction={runAction}
              onCreate={() => setCreateOpen(true)}
              onEdit={(instance) =>
                setEditing({
                  name: instance.name,
                  cpus: instance.cpus || 2,
                  memoryGB: instance.memoryGB || 2,
                  diskGB: instance.diskGB || 10,
                })
              }
              onRequestDelete={(instance) => setConfirmingDelete(instance)}
              copiedCommand={copiedCommand}
              onCopy={handleCopy}
              onViewKeys={(keys) => setViewingKeys(keys)}
            />
          ) : null}

          {bootstrapped && section === 'system' ? (
            <SystemScreen
              appVersion={state.appVersion}
              platform={state.platform}
              update={state.update}
              requirements={state.requirements}
              installJob={installJob}
              installing={Boolean(actionsBusy.install)}
              domainsBusy={Boolean(actionsBusy.domains)}
              hasCorePackages={hasCorePackages}
              onInstall={() => runAction('install', () => StartInstallPackages())}
              onFixDomains={() => runAction('domains', () => StartFixLocalDomains())}
              checkingUpdate={Boolean(actionsBusy['software-update'])}
              onCheckUpdate={() => runAction('software-update', () => CheckForUpdate())}
            />
          ) : null}

          {bootstrapped && section === 'activity' ? <ActivityScreen jobs={state.jobs} /> : null}
        </main>
      </div>

      <footer className="statusbar">
        <span className="statusbar-seg">
          <span className={`led ${runningCount > 0 ? 'led-running' : ''}`} aria-hidden />
          {state.instances.length} server{state.instances.length === 1 ? '' : 's'} ·{' '}
          {runningCount} running
        </span>
        {activeJobs.length > 0 ? (
          <>
            <span className="statusbar-div" />
            <span className="statusbar-seg statusbar-busy">
              <span className="spin" aria-hidden />
              {jobLabel(activeJobs[0].kind)}
              {activeJobs.length > 1 ? ` +${activeJobs.length - 1}` : ''}
            </span>
          </>
        ) : null}
        <span className="statusbar-gap" />
        <span className="statusbar-seg">
          {requirementsByName.get('multipass')?.installed ? 'multipass' : 'no VM backend'}
        </span>
        <span className="statusbar-div" />
        <span className="statusbar-seg">{state.platform || '—'}</span>
        <span className="statusbar-div" />
        <span className="statusbar-seg">
          <code>{state.appVersion ? `v${state.appVersion}` : '—'}</code>
        </span>
        <span className="statusbar-div" />
        <Zoomer zoom={zoom} onChange={changeZoom} />
      </footer>

      {activeJobs.length > 0 && section !== 'activity' ? (
        <div className="dock" role="status" aria-live="polite">
          {activeJobs.slice(0, 3).map((job) => (
            <div className="toast" key={job.id}>
              <span className="spin" aria-hidden />
              <div className="toast-text">
                <strong>{jobLabel(job.kind)}</strong>
                {job.target ? <small>{job.target}</small> : null}
                <p>{job.message}</p>
              </div>
            </div>
          ))}
          <button type="button" onClick={() => setSection('activity')}>
            Show all activity →
          </button>
        </div>
      ) : null}

      {createOpen ? (
        <CreateSheet
          onClose={() => setCreateOpen(false)}
          onSubmit={(values) =>
            runAction('create', () =>
              StartCreateSandbox({
                name: values.name.trim(),
                cpus: values.cpus,
                memoryGB: values.memoryGB,
                diskGB: values.diskGB,
                selfSigned: values.selfSigned,
              }),
            )
          }
          submitting={Boolean(actionsBusy.create)}
          createJob={createJob}
          hasCorePackages={hasCorePackages}
          onOpenSystem={() => {
            setCreateOpen(false);
            setSection('system');
          }}
        />
      ) : null}

      {editing ? (
        <ResizeSheet
          initialValues={editing}
          onClose={() => setEditing(null)}
          onSubmit={async (values) => {
            const key = `update-${values.name}`;
            await runAction(key, () =>
              StartUpdateSandbox({
                name: values.name,
                cpus: values.cpus,
                memoryGB: values.memoryGB,
                diskGB: values.diskGB,
              }),
            );
            setEditing(null);
          }}
          submitting={Boolean(actionsBusy[`update-${editing.name}`])}
        />
      ) : null}

      {viewingKeys ? (
        <SSHKeySheet keys={viewingKeys} onClose={() => setViewingKeys(null)} />
      ) : null}

      {confirmingDelete ? (
        <ConfirmSheet
          title={`Delete ${confirmingDelete.name}?`}
          body={
            <>
              <p>
                The VM, its snapshots, SSH keys, and TLS certificates are removed from this Mac.
              </p>
              <p className="muted">This cannot be undone.</p>
            </>
          }
          confirmLabel="Delete server"
          submitting={Boolean(actionsBusy[`destroy-${confirmingDelete.name}`])}
          onCancel={() => setConfirmingDelete(null)}
          onConfirm={async () => {
            const target = confirmingDelete;
            await runAction(`destroy-${target.name}`, () => StartDestroySandbox(target.name));
            setConfirmingDelete(null);
          }}
        />
      ) : null}
    </div>
  );
}

// ============================================================================
// Setup (first run)
// ============================================================================

function SetupScreen(props: {
  requirements: Requirement[];
  installJob: Job | undefined;
  installing: boolean;
  hasCorePackages: boolean;
  onInstall: () => void;
  onCreate: () => void;
}) {
  const byName = new Map<string, Requirement>();
  for (const req of props.requirements) byName.set(req.name, req);
  const completed = PACKAGES.filter((name) => byName.get(name)?.installed).length;

  return (
    <div className="detail-scroll">
      <header className="detail-head">
        <div className="detail-title">
          <h1>Set up vpsbox</h1>
          <p>Install the host packages, then boot your first local Ubuntu server.</p>
        </div>
      </header>

      <div className="pane">
        <div className="setup">
          <article className="card">
            <header className="card-head">
              <span className="step-no">1</span>
              <h3>Install host packages</h3>
              <StatusChip
                status={props.hasCorePackages ? 'ok' : 'pending'}
                label={props.hasCorePackages ? 'Done' : `${completed} of ${PACKAGES.length}`}
              />
            </header>
            <div className="card-body">
              <p>
                Multipass runs the VM, mkcert issues locally trusted HTTPS, and cloudflared
                exposes a server over a temporary public URL.
              </p>
              <Meter done={completed} total={PACKAGES.length} />
              <ul className="roster">
                {PACKAGES.map((name) => {
                  const req = byName.get(name);
                  const status = packageStatus(name, req, props.installJob);
                  return (
                    <li key={name}>
                      <div className="roster-text">
                        <strong>{PACKAGE_TITLES[name] ?? name}</strong>
                        <small>{status.details}</small>
                      </div>
                      <span className={`chip chip-${status.variant}`}>{status.label}</span>
                    </li>
                  );
                })}
              </ul>
            </div>
            <div className="card-foot">
              {props.hasCorePackages ? (
                <span className="hint">All packages are installed.</span>
              ) : (
                <Button variant="primary" size="md" onClick={props.onInstall} busy={props.installing}>
                  Install packages
                </Button>
              )}
            </div>
          </article>

          <article className={`card ${!props.hasCorePackages ? 'card-locked' : ''}`}>
            <header className="card-head">
              <span className={`step-no ${!props.hasCorePackages ? 'step-no-idle' : ''}`}>2</span>
              <h3>Create your first server</h3>
              <StatusChip
                status={props.hasCorePackages ? 'pending' : 'stopped'}
                label={props.hasCorePackages ? 'Ready when you are' : 'Waiting'}
              />
            </header>
            <div className="card-body">
              <p>
                vpsbox boots Ubuntu 24.04, installs Docker, generates an SSH key, and registers
                the server locally.
              </p>
              <ul className="facts">
                <li>2 vCPU, 2 GB memory, 10 GB disk by default</li>
                <li>
                  Reachable at <code>dev-1.vpsbox.local</code>
                </li>
                <li>Exports to Server Compass in one click</li>
              </ul>
            </div>
            <div className="card-foot">
              <Button
                variant="primary"
                size="md"
                icon="plus"
                onClick={props.onCreate}
                disabled={!props.hasCorePackages}
              >
                Create a server
              </Button>
            </div>
          </article>
        </div>
      </div>
    </div>
  );
}

// ============================================================================
// Servers screen
// ============================================================================

function ServersScreen(props: {
  instances: Sandbox[];
  selectedInstance: Sandbox | null;
  detailTab: DetailTab;
  setDetailTab: (tab: DetailTab) => void;
  actionsBusy: Record<string, boolean>;
  runAction: (key: string, fn: () => Promise<unknown>) => Promise<void>;
  onCreate: () => void;
  onEdit: (instance: Sandbox) => void;
  onRequestDelete: (instance: Sandbox) => void;
  copiedCommand: string;
  onCopy: (command: string) => void;
  onViewKeys: (keys: { privateKey: string; publicKey: string }) => void;
}) {
  const { instances, selectedInstance } = props;

  if (instances.length === 0) {
    return (
      <div className="detail-scroll">
        <Blank
          icon="server"
          title="No servers yet"
          description="Boot a local Ubuntu server to try a deploy tool without renting one."
          action={
            <Button variant="primary" size="md" icon="plus" onClick={props.onCreate}>
              Create a server
            </Button>
          }
        />
      </div>
    );
  }

  if (!selectedInstance) {
    return (
      <div className="detail-scroll">
        <Blank
          icon="server"
          title="Select a server"
          description="Pick one from the list on the left to inspect it."
        />
      </div>
    );
  }

  const isRunning = selectedInstance.status.toLowerCase() === 'running';
  const lifecycleKey = `lifecycle-${selectedInstance.name}`;
  const sshKey = `sshkey-${selectedInstance.name}`;
  const destroyKey = `destroy-${selectedInstance.name}`;

  return (
    <>
      <header className="detail-head">
        <div className="detail-title">
          <h1>
            <span className="truncate">{selectedInstance.name}</span>
            <StatusChip status={selectedInstance.status} />
          </h1>
          <p className="addr">
            {selectedInstance.username || 'root'}@{connectionHost(selectedInstance)}
            {selectedInstance.imported ? '  · imported' : ''}
          </p>
        </div>
        <div className="detail-ops">
          {isRunning ? (
            <Button
              icon="stop"
              busy={Boolean(props.actionsBusy[lifecycleKey])}
              onClick={() =>
                props.runAction(lifecycleKey, () => StartStopSandbox(selectedInstance.name))
              }
            >
              Stop
            </Button>
          ) : (
            <Button
              icon="play"
              busy={Boolean(props.actionsBusy[lifecycleKey])}
              onClick={() =>
                props.runAction(lifecycleKey, () => StartStartSandbox(selectedInstance.name))
              }
            >
              Start
            </Button>
          )}
          <Button
            variant="primary"
            icon="terminal"
            disabled={!selectedInstance.hasPrivateKey || !isRunning}
            onClick={() => void OpenShell(selectedInstance.name)}
            title={
              !selectedInstance.hasPrivateKey
                ? 'Generate an SSH key first'
                : !isRunning
                  ? 'Start the server first'
                  : undefined
            }
          >
            Open shell
          </Button>
        </div>
      </header>

      <div className="tabbar">
        <Segments<DetailTab>
          label="Server detail"
          value={props.detailTab}
          onChange={props.setDetailTab}
          options={[
            { id: 'overview', label: 'Overview' },
            { id: 'connect', label: 'Connect' },
            { id: 'logs', label: 'Logs' },
            { id: 'resources', label: 'Resources' },
          ]}
        />
      </div>

      {props.detailTab === 'overview' ? (
        <div className="detail-scroll">
          <div className="pane">
            <Inspector title="Instance">
              <Row label="Status">
                <StatusChip status={selectedInstance.status} />
              </Row>
              <Row label="Backend">{selectedInstance.backend || '—'}</Row>
              <Row label="Image">ubuntu 24.04</Row>
              <Row label="Created">{selectedInstance.createdAt || '—'}</Row>
            </Inspector>

            <Inspector title="Network">
              <Row label="Hostname">{selectedInstance.hostname || 'Pending'}</Row>
              <Row label="IP address">{selectedInstance.host || 'Pending'}</Row>
              <Row label="SSH user">{selectedInstance.username || 'root'}</Row>
              <Row label="SSH key">
                {selectedInstance.hasPrivateKey ? (
                  selectedInstance.privateKeyPath || 'Configured'
                ) : (
                  <span className="chip chip-pending">Not generated</span>
                )}
              </Row>
            </Inspector>
          </div>
        </div>
      ) : null}

      {props.detailTab === 'connect' ? (
        <div className="detail-scroll">
          <div className="pane">
            <div className="detail-ops">
              <Button
                icon="key"
                busy={Boolean(props.actionsBusy[sshKey])}
                onClick={() =>
                  props.runAction(sshKey, () => StartGenerateSSHKey(selectedInstance.name))
                }
              >
                {selectedInstance.hasPrivateKey ? 'Regenerate SSH key' : 'Generate SSH key'}
              </Button>
              {selectedInstance.hasPrivateKey ? (
                <>
                  <Button
                    onClick={async () => {
                      const keys = await ReadSSHKeys(selectedInstance.name);
                      props.onViewKeys(keys);
                    }}
                  >
                    View key
                  </Button>
                  <Button icon="folder" onClick={() => void RevealKeyFolder(selectedInstance.name)}>
                    Reveal folder
                  </Button>
                </>
              ) : null}
            </div>
            {!selectedInstance.hasPrivateKey ? (
              <Blank
                icon="key"
                title="No SSH key yet"
                description="Generate a key to unlock the shell and the copy commands below."
              />
            ) : (
              <div className="cmd-list">
                {buildQuickCommands(selectedInstance).map((command) => (
                  <article className="cmd" key={command.label}>
                    <div className="cmd-head">
                      <strong>{command.label}</strong>
                      <button
                        type="button"
                        className={`copy ${
                          props.copiedCommand === command.command ? 'copy-done' : ''
                        }`}
                        onClick={() => props.onCopy(command.command)}
                      >
                        {props.copiedCommand === command.command ? 'Copied' : 'Copy'}
                      </button>
                    </div>
                    <pre>
                      <code>{command.command}</code>
                    </pre>
                  </article>
                ))}
              </div>
            )}
          </div>
        </div>
      ) : null}

      {props.detailTab === 'resources' ? (
        <div className="detail-scroll">
          <div className="pane">
            <Inspector
              title="Allocation"
              action={
                <Button icon="sliders" onClick={() => props.onEdit(selectedInstance)}>
                  Resize
                </Button>
              }
            >
              <Row label="vCPU">{selectedInstance.cpus || 2}</Row>
              <Row label="Memory">{`${selectedInstance.memoryGB || 2} GB`}</Row>
              <Row label="Disk">{`${selectedInstance.diskGB || 10} GB`}</Row>
            </Inspector>

            <div className="danger">
              <div>
                <strong>Delete this server</strong>
                <p>
                  The VM, snapshots, SSH keys, and certificates are removed. This cannot be
                  undone.
                </p>
              </div>
              <Button
                variant="danger"
                busy={Boolean(props.actionsBusy[destroyKey])}
                onClick={() => props.onRequestDelete(selectedInstance)}
              >
                Delete server
              </Button>
            </div>
          </div>
        </div>
      ) : null}

      {props.detailTab === 'logs' ? <ServerLogsTab instance={selectedInstance} /> : null}
    </>
  );
}

// ============================================================================
// Logs — the traffic-list view
// ============================================================================

function ServerLogsTab({ instance }: { instance: Sandbox }) {
  const [logs, setLogs] = useState<ServerLogs | null>(null);
  const [category, setCategory] = useState<LogCategory>('all');
  const [query, setQuery] = useState('');
  const [loading, setLoading] = useState(false);
  const [loadError, setLoadError] = useState('');
  const requestID = useRef(0);
  const inFlight = useRef(false);
  const isRunning = instance.status.toLowerCase() === 'running';

  const load = useCallback(async () => {
    if (!isRunning || !instance.hasPrivateKey || inFlight.current) return;
    inFlight.current = true;
    const id = ++requestID.current;
    setLoading(true);
    try {
      const result = await GetServerLogs(instance.name);
      if (requestID.current !== id) return;
      setLogs(result as unknown as ServerLogs);
      setLoadError('');
    } catch (err) {
      if (requestID.current === id) setLoadError(toMessage(err));
    } finally {
      inFlight.current = false;
      if (requestID.current === id) setLoading(false);
    }
  }, [instance.hasPrivateKey, instance.name, isRunning]);

  useEffect(() => {
    setLogs(null);
    setLoadError('');
    setCategory('all');
    setQuery('');
    void load();
    const timer = window.setInterval(() => void load(), 5000);
    return () => {
      window.clearInterval(timer);
      requestID.current += 1;
    };
  }, [load]);

  if (!isRunning) {
    return (
      <div className="detail-scroll">
        <Blank
          icon="pulse"
          title="Start the server to read logs"
          description="Diagnostics stream from the running VM over SSH."
        />
      </div>
    );
  }

  if (!instance.hasPrivateKey) {
    return (
      <div className="detail-scroll">
        <Blank
          icon="key"
          title="An SSH key is required"
          description="Generate a key in the Connect tab so vpsbox can read diagnostics."
        />
      </div>
    );
  }

  const categories: { id: LogCategory; label: string }[] = [
    { id: 'all', label: 'All' },
    { id: 'system', label: 'System' },
    { id: 'network', label: 'Connections' },
    { id: 'route', label: 'Routes' },
    { id: 'docker', label: 'Docker' },
  ];
  const normalizedQuery = query.trim().toLowerCase();
  const entries = (logs?.entries ?? []).filter((entry) => {
    if (category !== 'all' && entry.category !== category) return false;
    if (!normalizedQuery) return true;
    return `${entry.source} ${entry.message} ${entry.level}`.toLowerCase().includes(normalizedQuery);
  });
  const countFor = (value: LogCategory) =>
    value === 'all'
      ? logs?.entries.length ?? 0
      : logs?.entries.filter((entry) => entry.category === value).length ?? 0;

  return (
    <div className="logs">
      <div className="logbar">
        <div className="filters" role="group" aria-label="Log category">
          {categories.map((item) => (
            <button
              key={item.id}
              type="button"
              className={`filter ${category === item.id ? 'filter-on' : ''}`}
              onClick={() => setCategory(item.id)}
            >
              {item.label} <b>{countFor(item.id)}</b>
            </button>
          ))}
        </div>
        <div className="logbar-right">
          <input
            className="search"
            type="search"
            value={query}
            onChange={(event) => setQuery(event.target.value)}
            placeholder="Filter"
            aria-label="Filter logs"
          />
          <Button icon="refresh" busy={loading} onClick={() => void load()}>
            Refresh
          </Button>
        </div>
      </div>

      <div className="log-note">
        <span>
          System journal, active TCP/UDP sockets, routing table, and recent Docker activity.
        </span>
        {logs?.fetchedAt ? <small>Updated {formatClockTime(logs.fetchedAt)}</small> : null}
      </div>

      {loadError ? (
        <div className="log-note">
          <span className="inline-error" role="alert">
            {loadError}
          </span>
        </div>
      ) : null}

      {!logs && loading ? (
        <div className="boot">
          <div className="spin spin-lg" aria-hidden />
          <p>Reading server diagnostics</p>
        </div>
      ) : null}

      {logs && entries.length === 0 ? (
        <Blank
          title="No matching entries"
          description="Try another category or clear the filter."
        />
      ) : null}

      {entries.length > 0 ? (
        <div className="logtable" role="table" aria-label="Server logs">
          <div className="logrow logrow-head" role="row">
            <span role="columnheader">Time</span>
            <span role="columnheader">Type</span>
            <span role="columnheader">Source</span>
            <span role="columnheader">Message</span>
          </div>
          {entries.map((entry) => (
            <div className={`logrow log-level-${entry.level}`} role="row" key={entry.id}>
              <time role="cell" dateTime={entry.timestamp}>
                {formatClockTime(entry.timestamp ?? '')}
              </time>
              <span role="cell" className={`tag tag-${entry.category}`}>
                {entry.category}
              </span>
              <span role="cell" className="src" title={entry.source}>
                {entry.source}
              </span>
              <code role="cell">{entry.message}</code>
            </div>
          ))}
        </div>
      ) : null}
    </div>
  );
}

// ============================================================================
// System screen
// ============================================================================

function SystemScreen(props: {
  appVersion: string;
  platform: string;
  update: UpdateInfo | undefined;
  requirements: Requirement[];
  installJob: Job | undefined;
  installing: boolean;
  domainsBusy: boolean;
  hasCorePackages: boolean;
  checkingUpdate: boolean;
  onInstall: () => void;
  onFixDomains: () => void;
  onCheckUpdate: () => void;
}) {
  const byName = new Map<string, Requirement>();
  for (const req of props.requirements) byName.set(req.name, req);
  const hosts = byName.get('hosts');
  const completed = PACKAGES.filter((name) => byName.get(name)?.installed).length;

  return (
    <div className="detail-scroll">
      <header className="detail-head">
        <div className="detail-title">
          <h1>Host environment</h1>
          <p>Dependencies vpsbox needs on this machine, and local hostname routing.</p>
        </div>
      </header>

      <div className="pane">
        <article className="card">
          <header className="card-head">
            <h3>Software update</h3>
            <StatusChip
              status={
                props.update?.error
                  ? 'error'
                  : props.update?.available
                    ? 'update available'
                    : props.update?.checkedAt
                      ? 'up to date'
                      : 'not checked'
              }
            />
          </header>
          <div className="card-body">
            <dl className="kv">
              <Row label="Installed">{props.appVersion ? `v${props.appVersion}` : '—'}</Row>
              <Row label="Latest">
                {props.update?.latest ? `v${props.update.latest}` : 'Not checked'}
              </Row>
              <Row label="Platform">{props.platform || '—'}</Row>
            </dl>
            {props.update?.error ? (
              <div className="inline-error" role="alert">
                {props.update.error}
              </div>
            ) : (
              <p>
                {props.update?.available
                  ? `vpsbox ${props.update.latest} is ready to download.`
                  : props.update?.checkedAt
                    ? 'vpsbox is up to date.'
                    : 'Check GitHub Releases for a newer desktop build.'}
              </p>
            )}
          </div>
          <div className="card-foot card-foot-spread">
            <span className="hint">
              {props.update?.checkedAt
                ? `Last checked ${formatRelative(props.update.checkedAt)}`
                : 'Never checked'}
            </span>
            <span className="detail-ops">
              <Button icon="refresh" busy={props.checkingUpdate} onClick={props.onCheckUpdate}>
                Check now
              </Button>
              {props.update?.available && props.update.url ? (
                <Button
                  variant="primary"
                  icon="download"
                  onClick={() => void OpenExternal(props.update!.url)}
                >
                  Download v{props.update.latest}
                </Button>
              ) : null}
            </span>
          </div>
        </article>

        <article className="card">
          <header className="card-head">
            <h3>Required packages</h3>
            <StatusChip
              status={props.hasCorePackages ? 'ok' : 'pending'}
              label={props.hasCorePackages ? 'Ready' : `${completed} of ${PACKAGES.length}`}
            />
          </header>
          <div className="card-body">
            <Meter done={completed} total={PACKAGES.length} />
            <ul className="roster">
              {PACKAGES.map((name) => {
                const req = byName.get(name);
                const status = packageStatus(name, req, props.installJob);
                return (
                  <li key={name}>
                    <div className="roster-text">
                      <strong>{PACKAGE_TITLES[name] ?? name}</strong>
                      <small>{req?.description || status.details}</small>
                    </div>
                    <span className={`chip chip-${status.variant}`}>{status.label}</span>
                  </li>
                );
              })}
            </ul>
          </div>
          <div className="card-foot">
            <Button variant="primary" onClick={props.onInstall} busy={props.installing} icon="download">
              {props.hasCorePackages ? 'Reinstall packages' : 'Install packages'}
            </Button>
          </div>
        </article>

        <article className="card">
          <header className="card-head">
            <h3>Local hostnames</h3>
            <StatusChip
              status={hosts?.installed ? 'ok' : 'pending'}
              label={hosts?.installed ? 'Configured' : 'Not configured'}
            />
          </header>
          <div className="card-body">
            <p>
              vpsbox writes a managed block to <code>/etc/hosts</code> so each server answers at{' '}
              <code>&lt;name&gt;.vpsbox.local</code>. Rewriting that file asks for your admin
              password.
            </p>
          </div>
          <div className="card-foot">
            <Button icon="globe" busy={props.domainsBusy} onClick={props.onFixDomains}>
              Rewrite /etc/hosts
            </Button>
          </div>
        </article>
      </div>
    </div>
  );
}

// ============================================================================
// Activity screen
// ============================================================================

function ActivityScreen({ jobs }: { jobs: Job[] }) {
  if (jobs.length === 0) {
    return (
      <div className="detail-scroll">
        <Blank
          icon="pulse"
          title="No activity yet"
          description="Installer runs, server lifecycle, and SSH operations land here."
        />
      </div>
    );
  }

  const running = jobs.filter((job) => job.state === 'running');
  const finished = jobs.filter((job) => job.state !== 'running');

  return (
    <div className="detail-scroll">
      <header className="detail-head">
        <div className="detail-title">
          <h1>Activity</h1>
          <p>
            {jobs.length} job{jobs.length === 1 ? '' : 's'} tracked this session.
          </p>
        </div>
      </header>

      <div className="pane">
        {running.length > 0 ? (
          <>
            <span className="group-label">In progress</span>
            <div className="feed">
              {running.map((job) => (
                <FeedRow key={job.id} job={job} />
              ))}
            </div>
          </>
        ) : null}

        <span className="group-label">Recent</span>
        {finished.length === 0 ? (
          <Blank title="Nothing finished yet" description="Completed jobs will land here." />
        ) : (
          <div className="feed">
            {finished.map((job) => (
              <FeedRow key={job.id} job={job} />
            ))}
          </div>
        )}
      </div>
    </div>
  );
}

function FeedRow({ job }: { job: Job }) {
  return (
    <div className="feed-row">
      <div className="feed-what">
        <strong>{jobLabel(job.kind)}</strong>
        <small>
          {job.target ? `${job.target} · ` : ''}
          {formatRelative(job.startedAt)}
        </small>
      </div>
      <p>{job.message || '—'}</p>
      <StatusChip status={job.state} />
    </div>
  );
}

// ============================================================================
// Sheets
// ============================================================================

function Sheet({
  title,
  subtitle,
  onClose,
  wide = false,
  role = 'dialog',
  children,
  footer,
}: {
  title: string;
  subtitle?: string;
  onClose: () => void;
  wide?: boolean;
  role?: 'dialog' | 'alertdialog';
  children: ReactNode;
  footer: ReactNode;
}) {
  useEffect(() => {
    const onKey = (event: KeyboardEvent) => {
      if (event.key === 'Escape') onClose();
    };
    window.addEventListener('keydown', onKey);
    return () => window.removeEventListener('keydown', onKey);
  }, [onClose]);

  return (
    <div className="scrim" onClick={onClose} role="presentation">
      <div
        className={`sheet ${wide ? 'sheet-wide' : ''}`}
        role={role}
        aria-modal="true"
        aria-label={title}
        onClick={(event) => event.stopPropagation()}
      >
        <header className="sheet-head">
          <div>
            <h2>{title}</h2>
            {subtitle ? <small>{subtitle}</small> : null}
          </div>
          <button type="button" className="sheet-x" onClick={onClose} aria-label="Close">
            <Icon name="close" size={13} />
          </button>
        </header>
        {children}
        <div className="sheet-foot">{footer}</div>
      </div>
    </div>
  );
}

function CreateSheet(props: {
  onClose: () => void;
  onSubmit: (values: CreateValues) => Promise<void>;
  submitting: boolean;
  createJob: Job | undefined;
  hasCorePackages: boolean;
  onOpenSystem: () => void;
}) {
  const [values, setValues] = useState<CreateValues>(DEFAULT_CREATE);
  const provisioning = Boolean(props.createJob && props.createJob.state === 'running');

  const handleSubmit = async (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    await props.onSubmit(values);
  };

  return (
    <Sheet
      title="Create a server"
      subtitle="Ubuntu 24.04 with Docker, booted locally by Multipass"
      onClose={props.onClose}
      wide
      footer={
        <>
          <Button onClick={props.onClose}>Cancel</Button>
          <Button
            variant="primary"
            busy={props.submitting || provisioning}
            disabled={!props.hasCorePackages}
            onClick={() => void props.onSubmit(values)}
          >
            {provisioning ? 'Provisioning' : 'Create server'}
          </Button>
        </>
      }
    >
      <form className="sheet-body" onSubmit={handleSubmit}>
        {!props.hasCorePackages ? (
          <div className="banner banner-warn">
            <div className="banner-body">
              <strong>Host packages are missing</strong>
              <p>Install Multipass, mkcert, and cloudflared first.</p>
            </div>
            <Button onClick={props.onOpenSystem}>Open environment</Button>
          </div>
        ) : null}

        {provisioning || props.createJob?.state === 'done' ? (
          <ol className="stages">
            {CREATE_STAGES.map((stage, index) => {
              const stageState = stageStateFor(index, props.createJob);
              return (
                <li className={`stage stage-${stageState}`} key={stage.id}>
                  <div className="stage-mark" aria-hidden>
                    {stageState === 'done' ? (
                      <Icon name="check" size={10} />
                    ) : stageState === 'active' ? (
                      <span className="stage-spin" />
                    ) : stageState === 'error' ? (
                      '!'
                    ) : (
                      index + 1
                    )}
                  </div>
                  <div className="stage-text">
                    <strong>{stage.title}</strong>
                    <small>
                      {stageState === 'active' && props.createJob
                        ? props.createJob.message
                        : stage.description}
                    </small>
                  </div>
                </li>
              );
            })}
          </ol>
        ) : null}

        <label className="field">
          <span>Name</span>
          <input
            value={values.name}
            placeholder="dev-1"
            onChange={(event) =>
              setValues((current) => ({ ...current, name: event.target.value }))
            }
          />
          <small>Leave empty to auto-name: dev-1, dev-2, …</small>
        </label>

        <div className="field-row">
          <label className="field">
            <span>vCPU</span>
            <input
              type="number"
              min={1}
              max={8}
              value={values.cpus}
              onChange={(event) =>
                setValues((current) => ({
                  ...current,
                  cpus: Number(event.target.value) || current.cpus,
                }))
              }
            />
          </label>
          <label className="field">
            <span>Memory (GB)</span>
            <input
              type="number"
              min={1}
              max={16}
              value={values.memoryGB}
              onChange={(event) =>
                setValues((current) => ({
                  ...current,
                  memoryGB: Number(event.target.value) || current.memoryGB,
                }))
              }
            />
          </label>
          <label className="field">
            <span>Disk (GB)</span>
            <input
              type="number"
              min={5}
              max={100}
              value={values.diskGB}
              onChange={(event) =>
                setValues((current) => ({
                  ...current,
                  diskGB: Number(event.target.value) || current.diskGB,
                }))
              }
            />
          </label>
        </div>

        <label className="check">
          <input
            type="checkbox"
            checked={values.selfSigned}
            onChange={(event) =>
              setValues((current) => ({ ...current, selfSigned: event.target.checked }))
            }
          />
          <span>Use a self-signed certificate instead of mkcert</span>
        </label>

        {/* Submits the form from the sheet footer button. */}
        <button type="submit" className="sr-only" tabIndex={-1} aria-hidden>
          Create server
        </button>
      </form>
    </Sheet>
  );
}

function ResizeSheet(props: {
  initialValues: EditValues;
  onClose: () => void;
  onSubmit: (values: EditValues) => Promise<void>;
  submitting: boolean;
}) {
  const [values, setValues] = useState<EditValues>(props.initialValues);

  return (
    <Sheet
      title={`Resize ${props.initialValues.name}`}
      subtitle="The VM stops, resizes, and restarts automatically"
      onClose={props.onClose}
      footer={
        <>
          <Button onClick={props.onClose}>Cancel</Button>
          <Button variant="primary" busy={props.submitting} onClick={() => void props.onSubmit(values)}>
            Save changes
          </Button>
        </>
      }
    >
      <div className="sheet-body">
        <div className="field-row">
          <label className="field">
            <span>vCPU</span>
            <input
              type="number"
              min={1}
              max={8}
              value={values.cpus}
              onChange={(event) =>
                setValues((current) => ({
                  ...current,
                  cpus: Number(event.target.value) || current.cpus,
                }))
              }
            />
          </label>
          <label className="field">
            <span>Memory (GB)</span>
            <input
              type="number"
              min={1}
              max={16}
              value={values.memoryGB}
              onChange={(event) =>
                setValues((current) => ({
                  ...current,
                  memoryGB: Number(event.target.value) || current.memoryGB,
                }))
              }
            />
          </label>
          <label className="field">
            <span>Disk (GB)</span>
            <input
              type="number"
              min={5}
              max={100}
              value={values.diskGB}
              onChange={(event) =>
                setValues((current) => ({
                  ...current,
                  diskGB: Number(event.target.value) || current.diskGB,
                }))
              }
            />
          </label>
        </div>
        <p className="muted">Disk can only grow. Shrinking it is rejected by Multipass.</p>
      </div>
    </Sheet>
  );
}

function SSHKeySheet(props: {
  keys: { privateKey: string; publicKey: string };
  onClose: () => void;
}) {
  const [tab, setTab] = useState<'public' | 'private'>('public');
  const [copied, setCopied] = useState(false);

  const content = tab === 'public' ? props.keys.publicKey : props.keys.privateKey;

  const handleCopy = () => {
    void navigator.clipboard.writeText(content);
    setCopied(true);
    setTimeout(() => setCopied(false), 1500);
  };

  return (
    <Sheet
      title="SSH key"
      subtitle="Stored under ~/.vpsbox/keys"
      onClose={props.onClose}
      wide
      footer={
        <>
          <Button onClick={props.onClose}>Close</Button>
          <Button variant="primary" onClick={handleCopy}>
            {copied ? 'Copied' : `Copy ${tab} key`}
          </Button>
        </>
      }
    >
      <div className="sheet-body">
        <Segments<'public' | 'private'>
          label="Key type"
          value={tab}
          onChange={setTab}
          options={[
            { id: 'public', label: 'Public key' },
            { id: 'private', label: 'Private key' },
          ]}
        />
        <div className="keybox">
          <pre>
            <code>{content}</code>
          </pre>
        </div>
        {tab === 'private' ? (
          <p className="muted">Keep this private key on your machine. Never paste it into a form.</p>
        ) : null}
      </div>
    </Sheet>
  );
}

function ConfirmSheet(props: {
  title: string;
  body: ReactNode;
  confirmLabel: string;
  submitting: boolean;
  onCancel: () => void;
  onConfirm: () => void;
}) {
  return (
    <Sheet
      title={props.title}
      onClose={props.onCancel}
      role="alertdialog"
      footer={
        <>
          <Button onClick={props.onCancel} disabled={props.submitting}>
            Cancel
          </Button>
          <Button variant="danger" onClick={props.onConfirm} busy={props.submitting}>
            {props.confirmLabel}
          </Button>
        </>
      }
    >
      <div className="sheet-body">{props.body}</div>
    </Sheet>
  );
}

export default App;
