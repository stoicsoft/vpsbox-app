import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import type { FormEvent, ReactNode } from 'react';
import './style.css';
import vpsboxIcon from './assets/images/vpsbox-icon.svg';
import {
  GetState,
  OpenExternal,
  OpenShell,
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

type AppState = {
  appVersion: string;
  platform: string;
  requirements: Requirement[];
  instances: Sandbox[];
  jobs: Job[];
};

type Section = 'servers' | 'system' | 'activity';
type DetailTab = 'overview' | 'connect' | 'resources';
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

function statusVariant(status: string): StatusVariant {
  switch (status.toLowerCase()) {
    case 'running':
    case 'done':
    case 'ok':
      return 'running';
    case 'stopped':
      return 'stopped';
    case 'error':
    case 'fail':
      return 'error';
    case 'pending':
    case 'starting':
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

async function copyToClipboard(text: string): Promise<boolean> {
  try {
    await navigator.clipboard.writeText(text);
    return true;
  } catch {
    return false;
  }
}

// ============================================================================
// Primitive components
// ============================================================================

function StatusPill({ status }: { status: string }) {
  const variant = statusVariant(status);
  return (
    <span className={`pill pill-${variant}`}>
      <span className="pill-dot" aria-hidden />
      {status || 'unknown'}
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
};

function Button({
  children,
  onClick,
  variant = 'ghost',
  busy = false,
  disabled = false,
  size = 'md',
  type = 'button',
  title,
}: ButtonProps) {
  return (
    <button
      type={type}
      className={`btn btn-${variant} btn-${size}`}
      onClick={onClick}
      disabled={disabled || busy}
      aria-busy={busy || undefined}
      title={title}
    >
      {busy ? <span className="btn-spinner" aria-hidden /> : null}
      <span className="btn-label">{children}</span>
    </button>
  );
}

function EmptyState({
  title,
  description,
  action,
}: {
  title: string;
  description: string;
  action?: ReactNode;
}) {
  return (
    <div className="empty">
      <strong>{title}</strong>
      <p>{description}</p>
      {action ? <div className="empty-action">{action}</div> : null}
    </div>
  );
}

function Stat({ label, value }: { label: string; value: ReactNode }) {
  return (
    <div className="stat">
      <dt>{label}</dt>
      <dd>{value}</dd>
    </div>
  );
}

function Tabs<T extends string>({
  value,
  options,
  onChange,
}: {
  value: T;
  options: { id: T; label: string }[];
  onChange: (id: T) => void;
}) {
  return (
    <div className="tabs" role="tablist">
      {options.map((option) => (
        <button
          key={option.id}
          type="button"
          role="tab"
          aria-selected={value === option.id}
          className={`tab ${value === option.id ? 'tab-active' : ''}`}
          onClick={() => onChange(option.id)}
        >
          {option.label}
        </button>
      ))}
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

  // When a create job finishes, close the dialog and select the new server.
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

  return (
    <div className="app">
      <aside className="sidebar">
        <div className="brand">
          <img className="brand-mark" src={vpsboxIcon} alt="" aria-hidden />
          <div className="brand-text">
            <strong>vpsbox</strong>
            <small>Local Ubuntu sandboxes</small>
          </div>
        </div>

        <nav className="nav" aria-label="Main">
          <button
            type="button"
            className={`nav-item ${section === 'servers' ? 'nav-item-active' : ''}`}
            onClick={() => setSection('servers')}
          >
            <span className="nav-icon" aria-hidden>
              ▦
            </span>
            <span>Servers</span>
            <span className="nav-count">{state.instances.length}</span>
          </button>
          <button
            type="button"
            className={`nav-item ${section === 'system' ? 'nav-item-active' : ''}`}
            onClick={() => setSection('system')}
          >
            <span className="nav-icon" aria-hidden>
              ⚙
            </span>
            <span>System</span>
            {!hasCorePackages && bootstrapped ? (
              <span className="nav-badge" title="Packages need install">
                !
              </span>
            ) : null}
          </button>
          <button
            type="button"
            className={`nav-item ${section === 'activity' ? 'nav-item-active' : ''}`}
            onClick={() => setSection('activity')}
          >
            <span className="nav-icon" aria-hidden>
              ≡
            </span>
            <span>Activity</span>
            {activeJobs.length > 0 ? (
              <span className="nav-count nav-count-busy">{activeJobs.length}</span>
            ) : null}
          </button>
        </nav>

        {section === 'servers' && state.instances.length > 0 ? (
          <div className="sidebar-list">
            <div className="sidebar-list-head">
              <span>Your servers</span>
              <button
                type="button"
                className="sidebar-add"
                onClick={() => setCreateOpen(true)}
                title="Create a new server"
                aria-label="Create a new server"
              >
                +
              </button>
            </div>
            <ul>
              {state.instances.map((instance) => (
                <li key={instance.name}>
                  <button
                    type="button"
                    className={`sidebar-item ${
                      selectedName === instance.name ? 'sidebar-item-active' : ''
                    }`}
                    onClick={() => {
                      setSelectedName(instance.name);
                      setSection('servers');
                      setDetailTab('overview');
                    }}
                  >
                    <span className="sidebar-item-name">{instance.name}</span>
                    <StatusPill status={instance.status} />
                  </button>
                </li>
              ))}
            </ul>
          </div>
        ) : null}

        <div className="sidebar-footer">
          <button
            type="button"
            className="sidebar-meta"
            onClick={() => void OpenExternal('https://servercompass.app')}
            title="Open Server Compass"
          >
            <span>Server Compass →</span>
            <small>Real VPS fleets, when you're ready</small>
          </button>
          <div className="sidebar-build">
            <span>{state.appVersion || '—'}</span>
            <span>{state.platform || '—'}</span>
          </div>
        </div>
      </aside>

      <main className="content">
        {error ? (
          <div className="banner banner-error" role="alert">
            <div className="banner-body">
              <strong>Error</strong>
              <p>{error}</p>
            </div>
            <button type="button" className="banner-dismiss" onClick={() => setError('')}>
              Dismiss
            </button>
          </div>
        ) : null}

        {!bootstrapped ? (
          <div className="loading-state">
            <div className="spinner" aria-hidden />
            <strong>Connecting to local backend…</strong>
            <p>Reading sandbox registry and host requirements.</p>
          </div>
        ) : null}

        {showFirstRun ? (
          <FirstRunPanel
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
          />
        ) : null}

        {bootstrapped && section === 'system' ? (
          <SystemScreen
            requirements={state.requirements}
            installJob={installJob}
            installing={Boolean(actionsBusy.install)}
            domainsBusy={Boolean(actionsBusy.domains)}
            hasCorePackages={hasCorePackages}
            onInstall={() => runAction('install', () => StartInstallPackages())}
            onFixDomains={() => runAction('domains', () => StartFixLocalDomains())}
          />
        ) : null}

        {bootstrapped && section === 'activity' ? <ActivityScreen jobs={state.jobs} /> : null}
      </main>

      {activeJobs.length > 0 && section !== 'activity' ? (
        <div className="toast-dock" role="status" aria-live="polite">
          {activeJobs.slice(0, 3).map((job) => (
            <div className="toast" key={job.id}>
              <div className="toast-spinner" aria-hidden />
              <div className="toast-body">
                <strong>{jobLabel(job.kind)}</strong>
                {job.target ? <small>{job.target}</small> : null}
                <p>{job.message}</p>
              </div>
            </div>
          ))}
          <button type="button" className="toast-link" onClick={() => setSection('activity')}>
            Show all activity →
          </button>
        </div>
      ) : null}

      {createOpen ? (
        <CreateDialog
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
        <EditDialog
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

      {confirmingDelete ? (
        <ConfirmDialog
          title="Delete this sandbox?"
          body={
            <>
              <p>
                <strong>{confirmingDelete.name}</strong> will be permanently deleted, along with
                its snapshots, SSH keys, and TLS certificates.
              </p>
              <p className="muted">This action cannot be undone.</p>
            </>
          }
          confirmLabel="Delete sandbox"
          variant="danger"
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
// First-run panel
// ============================================================================

function FirstRunPanel(props: {
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
  const percent = Math.round((completed / PACKAGES.length) * 100);

  return (
    <section className="panel">
      <header className="panel-head">
        <div>
          <span className="eyebrow">Welcome</span>
          <h1>Get vpsbox set up</h1>
          <p>Install the host packages, then create your first local Ubuntu sandbox.</p>
        </div>
      </header>

      <div className="setup-grid">
        <article className="setup-card">
          <div className="setup-card-head">
            <span className="setup-step">1</span>
            <div className="setup-card-title">
              <h3>Install host packages</h3>
              <p>
                Multipass for the VM, mkcert for trusted HTTPS, cloudflared for shares.
              </p>
            </div>
            <StatusPill status={props.hasCorePackages ? 'running' : 'pending'} />
          </div>
          <div className="progress">
            <div className="progress-bar">
              <div className="progress-fill" style={{ width: `${percent}%` }} />
            </div>
            <span>
              {completed} / {PACKAGES.length} ready
            </span>
          </div>
          <ul className="package-list">
            {PACKAGES.map((name) => {
              const req = byName.get(name);
              const status = packageStatus(name, req, props.installJob);
              return (
                <li key={name}>
                  <div>
                    <strong>{PACKAGE_TITLES[name] ?? name}</strong>
                    <small>{status.details}</small>
                  </div>
                  <span className={`pill pill-${status.variant}`}>
                    <span className="pill-dot" aria-hidden />
                    {status.label}
                  </span>
                </li>
              );
            })}
          </ul>
          {!props.hasCorePackages ? (
            <Button variant="primary" onClick={props.onInstall} busy={props.installing}>
              Install required packages
            </Button>
          ) : (
            <p className="muted">All packages are installed.</p>
          )}
        </article>

        <article className={`setup-card ${!props.hasCorePackages ? 'setup-card-locked' : ''}`}>
          <div className="setup-card-head">
            <span className="setup-step">2</span>
            <div className="setup-card-title">
              <h3>Create your first sandbox</h3>
              <p>
                vpsbox boots Ubuntu, installs Docker, generates SSH keys, and registers it
                locally.
              </p>
            </div>
            <StatusPill status={props.hasCorePackages ? 'pending' : 'stopped'} />
          </div>
          <ul className="bullets">
            <li>2 vCPU, 2 GB RAM, 10 GB disk by default</li>
            <li>
              Reachable at <code>dev-1.vpsbox.local</code>
            </li>
            <li>Export to Server Compass with one click</li>
          </ul>
          <Button variant="primary" onClick={props.onCreate} disabled={!props.hasCorePackages}>
            Create a sandbox
          </Button>
        </article>
      </div>
    </section>
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
}) {
  const { instances, selectedInstance } = props;

  if (instances.length === 0) {
    return (
      <section className="panel">
        <header className="panel-head">
          <div>
            <span className="eyebrow">Servers</span>
            <h1>No sandboxes yet</h1>
            <p>Spin up a local Ubuntu VPS to try a deploy tool without renting a server.</p>
          </div>
          <Button variant="primary" onClick={props.onCreate}>
            Create a sandbox
          </Button>
        </header>
      </section>
    );
  }

  if (!selectedInstance) {
    return (
      <section className="panel">
        <EmptyState
          title="Select a server"
          description="Choose a server from the sidebar to inspect it."
        />
      </section>
    );
  }

  const isRunning = selectedInstance.status.toLowerCase() === 'running';
  const lifecycleKey = `lifecycle-${selectedInstance.name}`;
  const sshKey = `sshkey-${selectedInstance.name}`;
  const destroyKey = `destroy-${selectedInstance.name}`;

  return (
    <section className="panel">
      <header className="panel-head panel-head-detail">
        <div className="panel-head-text">
          <span className="eyebrow">Server</span>
          <h1>{selectedInstance.name}</h1>
          <p>
            {selectedInstance.username || 'root'}@{connectionHost(selectedInstance)}
            {selectedInstance.imported ? ' · imported' : ''}
          </p>
        </div>
        <div className="head-actions">
          <StatusPill status={selectedInstance.status} />
          {isRunning ? (
            <Button
              busy={Boolean(props.actionsBusy[lifecycleKey])}
              onClick={() =>
                props.runAction(lifecycleKey, () => StartStopSandbox(selectedInstance.name))
              }
            >
              Stop
            </Button>
          ) : (
            <Button
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
            disabled={!selectedInstance.hasPrivateKey || !isRunning}
            onClick={() => void OpenShell(selectedInstance.name)}
            title={
              !selectedInstance.hasPrivateKey
                ? 'Generate an SSH key first'
                : !isRunning
                  ? 'Start the sandbox first'
                  : undefined
            }
          >
            Open shell
          </Button>
        </div>
      </header>

      <Tabs<DetailTab>
        value={props.detailTab}
        onChange={props.setDetailTab}
        options={[
          { id: 'overview', label: 'Overview' },
          { id: 'connect', label: 'Connect' },
          { id: 'resources', label: 'Resources' },
        ]}
      />

      {props.detailTab === 'overview' ? (
        <div className="tab-body">
          <dl className="stats">
            <Stat label="Status" value={<StatusPill status={selectedInstance.status} />} />
            <Stat label="Backend" value={selectedInstance.backend || '—'} />
            <Stat label="Hostname" value={selectedInstance.hostname || 'Pending'} />
            <Stat label="IP" value={selectedInstance.host || 'Pending'} />
            <Stat label="Created" value={selectedInstance.createdAt || '—'} />
          </dl>
        </div>
      ) : null}

      {props.detailTab === 'connect' ? (
        <div className="tab-body">
          <div className="connect-actions">
            <Button
              busy={Boolean(props.actionsBusy[sshKey])}
              onClick={() =>
                props.runAction(sshKey, () => StartGenerateSSHKey(selectedInstance.name))
              }
            >
              {selectedInstance.hasPrivateKey ? 'Regenerate SSH key' : 'Generate SSH key'}
            </Button>
          </div>
          {!selectedInstance.hasPrivateKey ? (
            <EmptyState
              title="No SSH key configured yet"
              description="Generate an SSH key first. After that the shell and scp commands will work."
            />
          ) : (
            <div className="commands">
              {buildQuickCommands(selectedInstance).map((command) => (
                <article className="command" key={command.label}>
                  <div className="command-head">
                    <strong>{command.label}</strong>
                    <button
                      type="button"
                      className="copy-btn"
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
      ) : null}

      {props.detailTab === 'resources' ? (
        <div className="tab-body">
          <dl className="stats">
            <Stat label="vCPU" value={selectedInstance.cpus || 2} />
            <Stat label="Memory" value={`${selectedInstance.memoryGB || 2} GB`} />
            <Stat label="Disk" value={`${selectedInstance.diskGB || 10} GB`} />
            <Stat label="Image" value="ubuntu 24.04" />
          </dl>
          <div className="resource-actions">
            <Button variant="primary" onClick={() => props.onEdit(selectedInstance)}>
              Resize
            </Button>
          </div>
          <div className="danger-zone">
            <div>
              <strong>Delete this sandbox</strong>
              <p>
                The VM, snapshots, SSH keys, and certificates will be removed. This cannot be
                undone.
              </p>
            </div>
            <Button
              variant="danger"
              busy={Boolean(props.actionsBusy[destroyKey])}
              onClick={() => props.onRequestDelete(selectedInstance)}
            >
              Delete sandbox
            </Button>
          </div>
        </div>
      ) : null}
    </section>
  );
}

// ============================================================================
// System screen
// ============================================================================

function SystemScreen(props: {
  requirements: Requirement[];
  installJob: Job | undefined;
  installing: boolean;
  domainsBusy: boolean;
  hasCorePackages: boolean;
  onInstall: () => void;
  onFixDomains: () => void;
}) {
  const byName = new Map<string, Requirement>();
  for (const req of props.requirements) byName.set(req.name, req);
  const hosts = byName.get('hosts');
  const completed = PACKAGES.filter((name) => byName.get(name)?.installed).length;
  const percent = Math.round((completed / PACKAGES.length) * 100);

  return (
    <section className="panel">
      <header className="panel-head">
        <div>
          <span className="eyebrow">System</span>
          <h1>Host environment</h1>
          <p>Install dependencies and fix local hostname routing.</p>
        </div>
      </header>

      <div className="card">
        <div className="card-head">
          <h3>Required packages</h3>
          <StatusPill status={props.hasCorePackages ? 'running' : 'pending'} />
        </div>
        <div className="progress">
          <div className="progress-bar">
            <div className="progress-fill" style={{ width: `${percent}%` }} />
          </div>
          <span>
            {completed} / {PACKAGES.length} ready
          </span>
        </div>
        <ul className="package-list">
          {PACKAGES.map((name) => {
            const req = byName.get(name);
            const status = packageStatus(name, req, props.installJob);
            return (
              <li key={name}>
                <div>
                  <strong>{PACKAGE_TITLES[name] ?? name}</strong>
                  <small>{req?.description || status.details}</small>
                </div>
                <span className={`pill pill-${status.variant}`}>
                  <span className="pill-dot" aria-hidden />
                  {status.label}
                </span>
              </li>
            );
          })}
        </ul>
        <div className="card-actions">
          <Button variant="primary" onClick={props.onInstall} busy={props.installing}>
            {props.hasCorePackages ? 'Reinstall packages' : 'Install packages'}
          </Button>
        </div>
      </div>

      <div className="card">
        <div className="card-head">
          <h3>Local hostnames</h3>
          <StatusPill status={hosts?.installed ? 'running' : 'pending'} />
        </div>
        <p className="card-copy">
          vpsbox writes a managed block to <code>/etc/hosts</code> so each sandbox is reachable as{' '}
          <code>&lt;name&gt;.vpsbox.local</code>. Updating this file requires admin privileges.
        </p>
        <div className="card-actions">
          <Button busy={props.domainsBusy} onClick={props.onFixDomains}>
            Fix /etc/hosts
          </Button>
        </div>
      </div>
    </section>
  );
}

// ============================================================================
// Activity screen
// ============================================================================

function ActivityScreen({ jobs }: { jobs: Job[] }) {
  if (jobs.length === 0) {
    return (
      <section className="panel">
        <header className="panel-head">
          <div>
            <span className="eyebrow">Activity</span>
            <h1>Activity log</h1>
            <p>Installer runs, sandbox lifecycle, and SSH operations show up here.</p>
          </div>
        </header>
        <EmptyState title="No activity yet" description="Actions you take will appear here." />
      </section>
    );
  }

  const running = jobs.filter((job) => job.state === 'running');
  const finished = jobs.filter((job) => job.state !== 'running');

  return (
    <section className="panel">
      <header className="panel-head">
        <div>
          <span className="eyebrow">Activity</span>
          <h1>Activity log</h1>
          <p>
            {jobs.length} job{jobs.length === 1 ? '' : 's'} tracked.
          </p>
        </div>
      </header>

      {running.length > 0 ? (
        <div className="activity-group">
          <h3>In progress</h3>
          {running.map((job) => (
            <ActivityRow key={job.id} job={job} />
          ))}
        </div>
      ) : null}

      <div className="activity-group">
        <h3>Recent</h3>
        {finished.length === 0 ? (
          <EmptyState title="Nothing finished yet" description="Recent activity will land here." />
        ) : (
          finished.map((job) => <ActivityRow key={job.id} job={job} />)
        )}
      </div>
    </section>
  );
}

function ActivityRow({ job }: { job: Job }) {
  return (
    <article className="activity">
      <div className="activity-meta">
        <strong>{jobLabel(job.kind)}</strong>
        <small>
          {job.target ? `${job.target} · ` : ''}
          {formatRelative(job.startedAt)}
        </small>
      </div>
      <p>{job.message || '—'}</p>
      <StatusPill status={job.state} />
    </article>
  );
}

// ============================================================================
// Create dialog
// ============================================================================

function CreateDialog(props: {
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
    <div className="overlay" onClick={props.onClose} role="presentation">
      <div
        className="slideover"
        role="dialog"
        aria-modal="true"
        aria-label="Create a new sandbox"
        onClick={(event) => event.stopPropagation()}
      >
        <header className="dialog-head">
          <div>
            <span className="eyebrow">New server</span>
            <h2>Create a sandbox</h2>
          </div>
          <button
            type="button"
            className="icon-btn"
            onClick={props.onClose}
            aria-label="Close dialog"
          >
            ×
          </button>
        </header>

        {!props.hasCorePackages ? (
          <div className="banner banner-warn">
            <div className="banner-body">
              <strong>Required packages aren't installed</strong>
              <p>Install Multipass, mkcert, and cloudflared from System first.</p>
            </div>
            <Button size="sm" onClick={props.onOpenSystem}>
              Open System
            </Button>
          </div>
        ) : null}

        {provisioning || props.createJob?.state === 'done' ? (
          <ol className="stages">
            {CREATE_STAGES.map((stage, index) => {
              const stageState = stageStateFor(index, props.createJob);
              return (
                <li className={`stage stage-${stageState}`} key={stage.id}>
                  <div className="stage-marker" aria-hidden>
                    {stageState === 'done'
                      ? '✓'
                      : stageState === 'active'
                        ? <span className="stage-spin" />
                        : stageState === 'error'
                          ? '!'
                          : index + 1}
                  </div>
                  <div>
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

        <form className="dialog-form" onSubmit={handleSubmit}>
          <label>
            <span>Name</span>
            <input
              value={values.name}
              placeholder="dev-1"
              onChange={(event) =>
                setValues((current) => ({ ...current, name: event.target.value }))
              }
            />
            <small>Leave empty to auto-name (dev-1, dev-2, …)</small>
          </label>
          <div className="form-row">
            <label>
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
            <label>
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
            <label>
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
          <label className="checkbox">
            <input
              type="checkbox"
              checked={values.selfSigned}
              onChange={(event) =>
                setValues((current) => ({ ...current, selfSigned: event.target.checked }))
              }
            />
            <span>Use a self-signed certificate instead of mkcert</span>
          </label>
          <div className="dialog-actions">
            <Button onClick={props.onClose}>Cancel</Button>
            <Button
              variant="primary"
              type="submit"
              busy={props.submitting || provisioning}
              disabled={!props.hasCorePackages}
            >
              {provisioning ? 'Provisioning…' : 'Create sandbox'}
            </Button>
          </div>
        </form>
      </div>
    </div>
  );
}

// ============================================================================
// Edit dialog
// ============================================================================

function EditDialog(props: {
  initialValues: EditValues;
  onClose: () => void;
  onSubmit: (values: EditValues) => Promise<void>;
  submitting: boolean;
}) {
  const [values, setValues] = useState<EditValues>(props.initialValues);

  const handleSubmit = async (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    await props.onSubmit(values);
  };

  return (
    <div className="overlay" onClick={props.onClose} role="presentation">
      <div
        className="modal"
        role="dialog"
        aria-modal="true"
        aria-label={`Resize ${props.initialValues.name}`}
        onClick={(event) => event.stopPropagation()}
      >
        <header className="dialog-head">
          <div>
            <span className="eyebrow">Resize</span>
            <h2>{props.initialValues.name}</h2>
          </div>
          <button
            type="button"
            className="icon-btn"
            onClick={props.onClose}
            aria-label="Close dialog"
          >
            ×
          </button>
        </header>
        <form className="dialog-form" onSubmit={handleSubmit}>
          <div className="form-row">
            <label>
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
            <label>
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
            <label>
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
          <p className="muted">The VM will be stopped, resized, and restarted automatically.</p>
          <div className="dialog-actions">
            <Button onClick={props.onClose}>Cancel</Button>
            <Button variant="primary" type="submit" busy={props.submitting}>
              Save changes
            </Button>
          </div>
        </form>
      </div>
    </div>
  );
}

// ============================================================================
// Confirm dialog
// ============================================================================

function ConfirmDialog(props: {
  title: string;
  body: ReactNode;
  confirmLabel: string;
  variant?: 'primary' | 'danger';
  submitting: boolean;
  onCancel: () => void;
  onConfirm: () => void;
}) {
  const variant = props.variant ?? 'primary';
  return (
    <div className="overlay" onClick={props.onCancel} role="presentation">
      <div
        className="modal modal-confirm"
        role="alertdialog"
        aria-modal="true"
        aria-label={props.title}
        onClick={(event) => event.stopPropagation()}
      >
        <header className="dialog-head">
          <div>
            <span className={`eyebrow ${variant === 'danger' ? 'eyebrow-danger' : ''}`}>
              {variant === 'danger' ? 'Confirm delete' : 'Confirm'}
            </span>
            <h2>{props.title}</h2>
          </div>
          <button
            type="button"
            className="icon-btn"
            onClick={props.onCancel}
            aria-label="Close dialog"
          >
            ×
          </button>
        </header>
        <div className="confirm-body">{props.body}</div>
        <div className="dialog-actions">
          <Button onClick={props.onCancel} disabled={props.submitting}>
            Cancel
          </Button>
          <Button
            variant={variant === 'danger' ? 'danger' : 'primary'}
            onClick={props.onConfirm}
            busy={props.submitting}
          >
            {props.confirmLabel}
          </Button>
        </div>
      </div>
    </div>
  );
}

export default App;
