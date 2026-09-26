import { ChangeEvent, DragEvent, FormEvent, ReactNode, useEffect, useMemo, useRef, useState } from 'react'
import { createRoot } from 'react-dom/client'
import './styles.css'
import './graph.css'
import './workspace.css'

const INSPECTION_API = import.meta.env.VITE_INSPECTION_API ?? 'http://localhost:8083'
const INTAKE_API = import.meta.env.VITE_INTAKE_API ?? 'http://localhost:8081'
const AI_SETTINGS_API = import.meta.env.VITE_AI_SETTINGS_API ?? 'http://localhost:8084'
const VISUAL_CHECKS = ['Spelling & copy', 'Text consistency', 'Visual abnormalities', 'Human anatomy', 'Object abnormalities', 'Alignment', 'Spacing', 'Logo', 'Readability']

type Finding = { defectType: string; severity: string; message: string }
type Task = { id: string; ruleId?: string; ruleName?: string; ruleType: string; state: string; findings: Finding[] }
type Job = { id: string; status: string; verdict?: string; tasks: Record<string, Task> }
type Specimen = { id: string; revision: number; objectKey: string; sizeBytes: number; widthPx: number; heightPx: number; format: string; mimeType: string }
type Rule = { id: string; name: string; instruction: string; source: 'project' | 'ad-hoc'; reference?: File; referenceObjectKey?: string; enabled: boolean }
type Check = { title: string; status: 'pass' | 'warning' | 'fail'; detail: string }
type Project = { id: string; name: string; code: string; description: string }
type Work = { id: string; projectId: string; name: string; description: string; updatedAt: string }
type AISettings = { agent: 'codex-api' | 'codex-cli' | 'claude-code-api' | 'claude-code-cli'; model?: string; apiKeyStored: boolean; cliAuthReady: boolean; updatedAt?: string }

const initialRules: Rule[] = [
  { id: 'copy', name: 'Copy & messaging', instruction: 'ตรวจคำสะกด ความสอดคล้องของข้อความ และ CTA ต้องอ่านได้ชัดเจน', source: 'project', enabled: true },
  { id: 'brand', name: 'Brand composition', instruction: 'โลโก้ต้องมองเห็นชัด มีพื้นที่หายใจ และเลย์เอาต์ต้องเป็นไปตามแนวทางแบรนด์', source: 'project', enabled: true },
  { id: 'accessibility', name: 'Readability', instruction: 'ข้อความสำคัญต้องมี contrast เพียงพอ อ่านได้บนมือถือ และไม่มีองค์ประกอบบังข้อความ', source: 'project', enabled: true },
]

const initialProjects: Project[] = [{ id: 'brand-campaigns', name: 'Brand campaigns', code: 'BRAND', description: 'Campaign artwork and brand communication.' }]
const initialWorks: Work[] = [{ id: 'summer-seafood', projectId: 'brand-campaigns', name: 'Summer seafood — final artwork', description: 'Social launch', updatedAt: new Date().toISOString() }]

function loadSaved<T>(key: string, fallback: T): T {
  try { const saved = window.localStorage.getItem(key); return saved ? JSON.parse(saved) as T : fallback } catch { return fallback }
}

function App() {
  const [projects, setProjects] = useState<Project[]>(() => loadSaved('ai-qc-projects', initialProjects))
  const [works, setWorks] = useState<Work[]>(() => loadSaved('ai-qc-works', initialWorks))
  const [currentProjectId, setCurrentProjectId] = useState(() => loadSaved('ai-qc-current-project', initialProjects[0].id))
  const [inspectionWorkId, setInspectionWorkId] = useState<string | null>(null)
  const [settingsOpen, setSettingsOpen] = useState(false)

  useEffect(() => { window.localStorage.setItem('ai-qc-projects', JSON.stringify(projects)) }, [projects])
  useEffect(() => { window.localStorage.setItem('ai-qc-works', JSON.stringify(works)) }, [works])
  useEffect(() => { window.localStorage.setItem('ai-qc-current-project', currentProjectId) }, [currentProjectId])

  const currentProject = projects.find(project => project.id === currentProjectId) ?? projects[0]
  const inspectionWork = works.find(work => work.id === inspectionWorkId)
  const saveProject = (draft: Omit<Project, 'id'>, id?: string) => {
    if (id) setProjects(items => items.map(item => item.id === id ? { ...draft, id } : item))
    else {
      const project = { ...draft, id: crypto.randomUUID() }
      setProjects(items => [...items, project]); setCurrentProjectId(project.id)
    }
  }
  const deleteProject = (id: string) => {
    const project = projects.find(item => item.id === id)
    if (!project || !window.confirm(`Delete “${project.name}” and all of its works?`)) return
    const remaining = projects.filter(item => item.id !== id)
    setProjects(remaining); setWorks(items => items.filter(item => item.projectId !== id))
    if (currentProjectId === id) setCurrentProjectId(remaining[0]?.id ?? '')
  }
  const saveWork = (draft: Omit<Work, 'id' | 'projectId' | 'updatedAt'>, id?: string) => {
    if (!currentProject) return
    if (id) setWorks(items => items.map(item => item.id === id ? { ...item, ...draft, updatedAt: new Date().toISOString() } : item))
    else setWorks(items => [...items, { ...draft, id: crypto.randomUUID(), projectId: currentProject.id, updatedAt: new Date().toISOString() }])
  }
  const deleteWork = (id: string) => {
    const work = works.find(item => item.id === id)
    if (work && window.confirm(`Delete “${work.name}”?`)) setWorks(items => items.filter(item => item.id !== id))
  }

  if (inspectionWork && currentProject) return <><InspectionScreen key={inspectionWork.id} project={currentProject} work={inspectionWork} onBack={() => setInspectionWorkId(null)} onConfigure={() => setSettingsOpen(true)}/>{settingsOpen && <AISettingsModal onClose={() => setSettingsOpen(false)}/>}</>
  return <><WorkspaceHome
    projects={projects} works={works} currentProject={currentProject}
    onSelectProject={setCurrentProjectId} onSaveProject={saveProject} onDeleteProject={deleteProject}
    onSaveWork={saveWork} onDeleteWork={deleteWork} onInspect={setInspectionWorkId} onConfigure={() => setSettingsOpen(true)}
  />{settingsOpen && <AISettingsModal onClose={() => setSettingsOpen(false)}/>}</>
}

function WorkspaceHome({ projects, works, currentProject, onSelectProject, onSaveProject, onDeleteProject, onSaveWork, onDeleteWork, onInspect, onConfigure }: {
  projects: Project[]; works: Work[]; currentProject?: Project; onSelectProject: (id: string) => void
  onSaveProject: (draft: Omit<Project, 'id'>, id?: string) => void; onDeleteProject: (id: string) => void
  onSaveWork: (draft: Omit<Work, 'id' | 'projectId' | 'updatedAt'>, id?: string) => void; onDeleteWork: (id: string) => void; onInspect: (id: string) => void; onConfigure: () => void
}) {
  const [projectEditor, setProjectEditor] = useState<'new' | Project | null>(null)
  const [workEditor, setWorkEditor] = useState<'new' | Work | null>(null)
  const projectWorks = currentProject ? works.filter(work => work.projectId === currentProject.id) : []

  return <main className="app-shell workspace-home">
    <header className="topbar"><div className="brand-lockup"><div className="mark">✓</div><div><p className="eyebrow">Production companion</p><h1>AI Graphic QC</h1></div></div><div className="topbar-actions"><button className="settings-button" type="button" onClick={onConfigure}>AI connection</button><div className="review-notice"><span />Set up a project and work before inspection</div></div></header>
    <section className="home-hero"><div><p className="step">WORKSPACE</p><h2>Projects &amp; work</h2><p>Create the place your artwork belongs, then open a work to build its inspection plan.</p></div><button className="primary-button" type="button" onClick={() => setProjectEditor('new')}>＋ New project</button></section>
    <section className="workspace-management">
      <aside className="project-list panel"><div className="section-heading"><p className="step">PROJECTS</p><h2>Your projects</h2></div>{projects.length ? <div className="project-cards">{projects.map(project => <article key={project.id} className={`project-card ${project.id === currentProject?.id ? 'selected' : ''}`}><button className="project-select" type="button" onClick={() => onSelectProject(project.id)}><span className="project-monogram">{project.name.slice(0, 1).toUpperCase()}</span><span><strong>{project.name}</strong><small>{project.code || 'NO CODE'}</small></span></button><div className="card-actions"><button type="button" aria-label={`Edit ${project.name}`} onClick={() => setProjectEditor(project)}>Edit</button><button type="button" className="danger-button" aria-label={`Delete ${project.name}`} onClick={() => onDeleteProject(project.id)}>Delete</button></div></article>)}</div> : <EmptyState title="No projects yet" detail="Create a project to start organizing work." action="New project" onAction={() => setProjectEditor('new')}/>}</aside>
      <section className="work-list panel">{currentProject ? <><div className="work-list-heading"><div><p className="step">PROJECT / {currentProject.code || 'UNTITLED'}</p><h2>{currentProject.name}</h2><p>{currentProject.description || 'No project description yet.'}</p></div><button className="primary-button" type="button" onClick={() => setWorkEditor('new')}>＋ New work</button></div><div className="works-grid">{projectWorks.length ? projectWorks.map(work => <article className="work-card" key={work.id}><div className="work-card-top"><span className="work-icon">◫</span><span className="work-date">Updated {formatRelativeDate(work.updatedAt)}</span></div><h3>{work.name}</h3><p>{work.description || 'No work description yet.'}</p><div className="work-card-footer"><button className="inspect-link" type="button" onClick={() => onInspect(work.id)}>Open inspection →</button><span><button type="button" onClick={() => setWorkEditor(work)}>Edit</button><button type="button" className="danger-button" onClick={() => onDeleteWork(work.id)}>Delete</button></span></div></article>) : <EmptyState title="No work in this project" detail="Create a work, then add the rules and artwork to inspect." action="New work" onAction={() => setWorkEditor('new')}/>}</div></> : <EmptyState title="Choose or create a project" detail="Projects keep related work and rules together." action="New project" onAction={() => setProjectEditor('new')}/>}</section>
    </section>
    {projectEditor && <ProjectEditor project={projectEditor === 'new' ? undefined : projectEditor} onCancel={() => setProjectEditor(null)} onSave={(draft) => { onSaveProject(draft, projectEditor === 'new' ? undefined : projectEditor.id); setProjectEditor(null) }}/>} 
    {workEditor && <WorkEditor work={workEditor === 'new' ? undefined : workEditor} onCancel={() => setWorkEditor(null)} onSave={(draft) => { onSaveWork(draft, workEditor === 'new' ? undefined : workEditor.id); setWorkEditor(null) }}/>} 
  </main>
}

function EmptyState({ title, detail, action, onAction }: { title: string; detail: string; action: string; onAction: () => void }) { return <div className="empty-state"><div className="empty-state-icon">＋</div><strong>{title}</strong><p>{detail}</p><button className="text-button" type="button" onClick={onAction}>{action}</button></div> }

function ProjectEditor({ project, onCancel, onSave }: { project?: Project; onCancel: () => void; onSave: (draft: Omit<Project, 'id'>) => void }) {
  const [name, setName] = useState(project?.name ?? '')
  const [code, setCode] = useState(project?.code ?? '')
  const [description, setDescription] = useState(project?.description ?? '')
  return <Modal title={project ? 'Edit project' : 'New project'} onClose={onCancel}><form onSubmit={event => { event.preventDefault(); if (name.trim()) onSave({ name: name.trim(), code: code.trim().toUpperCase(), description: description.trim() }) }}><Field label="Project name" value={name} onChange={setName} placeholder="e.g. Brand campaigns" required/><Field label="Project code" value={code} onChange={setCode} placeholder="e.g. BRAND"/><Field label="Description" value={description} onChange={setDescription} placeholder="What is this project for?" textarea/><ModalActions onCancel={onCancel} submit={project ? 'Save changes' : 'Create project'}/></form></Modal>
}

function WorkEditor({ work, onCancel, onSave }: { work?: Work; onCancel: () => void; onSave: (draft: Omit<Work, 'id' | 'projectId' | 'updatedAt'>) => void }) {
  const [name, setName] = useState(work?.name ?? '')
  const [description, setDescription] = useState(work?.description ?? '')
  return <Modal title={work ? 'Edit work' : 'New work'} onClose={onCancel}><form onSubmit={event => { event.preventDefault(); if (name.trim()) onSave({ name: name.trim(), description: description.trim() }) }}><Field label="Work name" value={name} onChange={setName} placeholder="e.g. Summer campaign final artwork" required/><Field label="Description" value={description} onChange={setDescription} placeholder="e.g. Social launch" textarea/><ModalActions onCancel={onCancel} submit={work ? 'Save changes' : 'Create work'}/></form></Modal>
}

function Modal({ title, onClose, children }: { title: string; onClose: () => void; children: ReactNode }) { return <div className="modal-backdrop" role="presentation" onMouseDown={onClose}><section className="modal-card" role="dialog" aria-modal="true" aria-label={title} onMouseDown={event => event.stopPropagation()}><div className="modal-heading"><h2>{title}</h2><button className="remove-rule" type="button" aria-label="Close" onClick={onClose}>×</button></div>{children}</section></div> }
function Field({ label, value, onChange, placeholder, required, textarea }: { label: string; value: string; onChange: (value: string) => void; placeholder: string; required?: boolean; textarea?: boolean }) { return <label className="form-field">{label}{textarea ? <textarea rows={3} value={value} placeholder={placeholder} onChange={event => onChange(event.target.value)}/> : <input value={value} placeholder={placeholder} required={required} onChange={event => onChange(event.target.value)}/>}</label> }
function ModalActions({ onCancel, submit }: { onCancel: () => void; submit: string }) { return <div className="modal-actions"><button className="secondary-button" type="button" onClick={onCancel}>Cancel</button><button className="primary-button" type="submit">{submit}</button></div> }

function AISettingsModal({ onClose }: { onClose: () => void }) {
  const [settings, setSettings] = useState<AISettings | null>(null)
  const [agent, setAgent] = useState<AISettings['agent']>('codex-cli')
  const [model, setModel] = useState('')
  const [apiKey, setAPIKey] = useState('')
  const [archive, setArchive] = useState<File | null>(null)
  const [accessToken, setAccessToken] = useState(() => window.sessionStorage.getItem('ai-qc-settings-token') ?? '')
  const [message, setMessage] = useState('')
  const [loading, setLoading] = useState(false)
  const isCLI = agent === 'codex-cli' || agent === 'claude-code-cli'
  const isAPI = !isCLI
  const requestHeaders = (): Record<string, string> => accessToken ? { 'X-Settings-Token': accessToken } : {}
  const load = async () => {
    setLoading(true); setMessage('')
    try {
      const response = await fetch(`${AI_SETTINGS_API}/api/v1/ai-settings`, { headers: requestHeaders() })
      if (!response.ok) throw new Error(await errorMessage(response))
      const current = await response.json() as AISettings
      setSettings(current); setAgent(current.agent); setModel(current.model ?? '')
    } catch (cause) { setMessage(cause instanceof Error ? cause.message : 'Could not load AI settings.') } finally { setLoading(false) }
  }
  useEffect(() => { void load() }, [])
  const save = async (event: FormEvent) => {
    event.preventDefault(); setLoading(true); setMessage('')
    try {
      if (isCLI && !archive && !settings?.cliAuthReady) throw new Error('Upload the CLI authentication ZIP before saving this connection.')
      if (isAPI && !apiKey.trim() && !settings?.apiKeyStored) throw new Error('Enter the API key for this connection.')
      const update = await fetch(`${AI_SETTINGS_API}/api/v1/ai-settings`, { method: 'PUT', headers: { 'Content-Type': 'application/json', ...requestHeaders() }, body: JSON.stringify({ agent, model, apiKey }) })
      if (!update.ok) throw new Error(await errorMessage(update))
      let current = await update.json() as AISettings
      if (isCLI && archive) {
        const form = new FormData(); form.set('agent', agent); form.set('archive', archive)
        const upload = await fetch(`${AI_SETTINGS_API}/api/v1/ai-settings/cli-auth`, { method: 'POST', headers: requestHeaders(), body: form })
        if (!upload.ok) throw new Error(await errorMessage(upload))
        current = await upload.json() as AISettings
      }
      if (accessToken) window.sessionStorage.setItem('ai-qc-settings-token', accessToken)
      else window.sessionStorage.removeItem('ai-qc-settings-token')
      setSettings(current); setArchive(null); setAPIKey(''); setMessage('Saved. This connection will be used for the next QC run.')
    } catch (cause) { setMessage(cause instanceof Error ? cause.message : 'Could not save AI settings.') } finally { setLoading(false) }
  }
  return <Modal title="AI connection" onClose={onClose}><form className="ai-settings-form" onSubmit={save}>
    <p className="settings-note">Credentials are sent only to the QC service and are never stored in this browser. Choose how this deployment should run AI checks.</p>
    <label className="form-field">Connection type<select value={agent} onChange={event => setAgent(event.target.value as AISettings['agent'])}><option value="codex-cli">Codex CLI — upload auth ZIP</option><option value="codex-api">OpenAI API key</option><option value="claude-code-cli">Claude Code CLI — upload auth ZIP</option><option value="claude-code-api">Anthropic API key</option></select></label>
    <Field label="Model (optional)" value={model} onChange={setModel} placeholder={isCLI ? 'Use the CLI default' : 'Use the provider default'}/>
    {isAPI ? <label className="form-field">API key<input type="password" autoComplete="off" value={apiKey} placeholder={settings?.apiKeyStored ? 'A key is already saved — enter a new one to replace it' : 'Paste API key'} onChange={event => setAPIKey(event.target.value)}/></label> : <label className="form-field">CLI authentication ZIP<input type="file" accept="application/zip,.zip" onChange={event => setArchive(event.target.files?.[0] ?? null)}/><small className="field-help">Export the CLI profile folder as ZIP (for example, the contents of <code>{agent === 'codex-cli' ? '.codex' : '.claude'}</code>). ZIP only; max 25 MB. {settings?.cliAuthReady && !archive ? 'An authentication ZIP is already configured.' : ''}</small></label>}
    <label className="form-field">Settings access token <small>(only if your host configured one)</small><input type="password" autoComplete="off" value={accessToken} placeholder="Optional deployment token" onChange={event => setAccessToken(event.target.value)}/></label>
    {message && <p className={`settings-message ${message.startsWith('Saved') ? 'success' : ''}`}>{message}</p>}
    <div className="modal-actions"><button className="secondary-button" type="button" onClick={() => void load()} disabled={loading}>Reload</button><button className="primary-button" type="submit" disabled={loading}>{loading ? 'Saving…' : 'Save connection'}</button></div>
  </form></Modal>
}

function InspectionScreen({ project, work, onBack, onConfigure }: { project: Project; work: Work; onBack: () => void; onConfigure: () => void }) {
  const [projectRules, setProjectRules] = useState<Rule[]>(() => {
    const saved = window.localStorage.getItem(`ai-qc-project-rules:${project.id}`)
    try { return saved ? JSON.parse(saved) as Rule[] : initialRules } catch { return initialRules }
  })
  const [workRules, setWorkRules] = useState<Rule[]>([initialRules[0], initialRules[1]])
  const [draftInstruction, setDraftInstruction] = useState('')
  const [file, setFile] = useState<File | null>(null)
  const [preview, setPreview] = useState('')
  const [specimen, setSpecimen] = useState<Specimen | null>(null)
  const [job, setJob] = useState<Job | null>(null)
  const [running, setRunning] = useState(false)
  const [error, setError] = useState('')
  const referenceInputs = useRef<Record<string, HTMLInputElement | null>>({})
  const runInFlight = useRef(false)

  useEffect(() => () => { if (preview) URL.revokeObjectURL(preview) }, [preview])
  useEffect(() => {
    const reusable = projectRules.map(({ reference: _reference, referenceObjectKey: _referenceObjectKey, ...rule }) => rule)
    window.localStorage.setItem(`ai-qc-project-rules:${project.id}`, JSON.stringify(reusable))
  }, [projectRules, project.id])
  useEffect(() => {
    if (!job || job.status === 'EVALUATED') return
    const timer = window.setInterval(async () => {
      const response = await fetch(`${INSPECTION_API}/api/v1/inspections/${job.id}`)
      if (response.ok) setJob(await response.json() as Job)
    }, 600)
    return () => window.clearInterval(timer)
  }, [job])

  const visualChecks = useMemo(() => job ? buildVisualChecks(job, workRules) : [], [job, workRules])
  const findings = useMemo(() => job ? Object.values(job.tasks).flatMap(task => (task.findings ?? []).map(finding => ({ ...finding, ruleName: task.ruleName ?? labelFor(task.ruleType) }))) : [], [job])
  const verdict = job?.verdict === 'PASSED' ? 'PASS' : job?.verdict === 'FAILED' ? 'FAIL' : job?.status === 'EVALUATED' ? 'WARNING' : '—'
  const qcInProgress = running || (job !== null && job.status !== 'EVALUATED')

  const selectArtwork = (selected: File | undefined) => {
    if (!selected) return
    if (!['image/png', 'image/jpeg'].includes(selected.type)) { setError('Please choose a PNG, JPG or JPEG artwork file.'); return }
    if (preview) URL.revokeObjectURL(preview)
    setError(''); setJob(null); setFile(selected); setSpecimen(null)
    const url = URL.createObjectURL(selected); setPreview(url)
    const image = new Image()
    image.onload = () => setSpecimen({ id: selected.name, revision: 1, objectKey: '', sizeBytes: selected.size, widthPx: image.naturalWidth, heightPx: image.naturalHeight, format: selected.type === 'image/png' ? 'png' : 'jpeg', mimeType: selected.type })
    image.src = url
  }
  const addRuleToWork = (rule: Rule) => setWorkRules(current => current.some(item => item.id === rule.id) ? current : [...current, rule])
  const removeWorkRule = (id: string) => setWorkRules(current => current.filter(rule => rule.id !== id))
  const handleRuleDrop = (event: DragEvent<HTMLElement>) => { event.preventDefault(); const rule = projectRules.find(item => item.id === event.dataTransfer.getData('application/qc-rule')); if (rule) addRuleToWork(rule) }
  const attachReference = (id: string, selected?: File) => {
    if (!selected) return
    if (!['image/png', 'image/jpeg'].includes(selected.type)) { setError('Rule references must be PNG, JPG or JPEG.'); return }
    setProjectRules(current => current.map(rule => rule.id === id ? { ...rule, reference: selected, referenceObjectKey: undefined } : rule))
    setWorkRules(current => current.map(rule => rule.id === id ? { ...rule, reference: selected, referenceObjectKey: undefined } : rule))
  }
  const createProjectRule = () => {
    const instruction = window.prompt('Describe the reusable rule for this project')?.trim()
    if (!instruction) return
    const name = window.prompt('Give this rule a short name', `Project rule ${projectRules.length + 1}`)?.trim() || `Project rule ${projectRules.length + 1}`
    setProjectRules(current => [...current, { id: crypto.randomUUID(), name, instruction, source: 'project', enabled: true }])
  }
  const extractRules = () => {
    const parts = draftInstruction.split(/\n+|(?<=[.!?])\s+/).map(value => value.replace(/^[-•\d.\s]+/, '').trim()).filter(value => value.length > 2)
    if (!parts.length) return
    setWorkRules(current => [...current, ...parts.map((instruction, index): Rule => ({ id: crypto.randomUUID(), name: `Work instruction ${index + 1}`, instruction, source: 'ad-hoc', enabled: true }))])
    setDraftInstruction('')
  }
  const runQc = async () => {
    if (!file || !workRules.length || runInFlight.current || qcInProgress) return
    runInFlight.current = true
    setError(''); setRunning(true); setJob(null)
    try {
      const uploaded = await uploadImage(file)
      const preparedRules = await Promise.all(workRules.map(async rule => !rule.reference || rule.referenceObjectKey ? rule : { ...rule, referenceObjectKey: (await uploadImage(rule.reference)).objectKey }))
      setWorkRules(preparedRules); setSpecimen(uploaded)
      const inspect = await fetch(`${INSPECTION_API}/api/v1/inspections`, { method: 'POST', headers: { 'Content-Type': 'application/json', 'Idempotency-Key': crypto.randomUUID() }, body: JSON.stringify({ tenantId: 'demo-tenant', projectId: project.id, specimen: uploaded, context: { workId: work.id }, rules: preparedRules.map(ruleFor) }) })
      if (!inspect.ok) throw new Error(await errorMessage(inspect))
      const created = await inspect.json() as { jobId?: unknown; status?: unknown }
      if (typeof created.jobId !== 'string' || !created.jobId) throw new Error(`Inspection API returned no jobId. Response status: ${String(created.status ?? 'unknown')}`)
      setJob({ id: created.jobId, status: 'ANALYZING', tasks: {} })
    } catch (cause) { setError(cause instanceof Error ? cause.message : 'Unable to upload artwork.') } finally { runInFlight.current = false; setRunning(false) }
  }

  return <main className="app-shell">
    <header className="topbar"><div className="brand-lockup"><div className="mark">✓</div><div><p className="eyebrow">Production companion</p><h1>AI Graphic QC</h1></div></div><button className="back-button" type="button" onClick={onBack}>← Projects &amp; work</button><div className="project-switcher"><span>PROJECT</span><strong>{project.name}</strong></div><div className="topbar-actions"><button className="settings-button" type="button" onClick={onConfigure}>AI connection</button><div className="review-notice"><span />AI-assisted first pass · human review required</div></div></header>
    {error && <p className="error-message">{error}</p>}
    <section className="work-header"><div><p className="step">WORK / {work.description.toUpperCase() || 'UNTITLED'}</p><h2>{work.name}</h2><p>Compose the project rules needed for this particular deliverable.</p></div><div className="work-rule-count"><strong>{workRules.length}</strong><span>active rules</span></div></section>
    <section className="workspace">
      <aside className="rule-library panel"><div className="section-heading"><p className="step">01 / PROJECT</p><div className="library-title"><h2>Rule library</h2><button type="button" onClick={createProjectRule}>＋ New rule</button></div><p>Drag a reusable rule into this work.</p></div><div className="rule-library-list">{projectRules.map(rule => <article key={rule.id} draggable onDragStart={event => event.dataTransfer.setData('application/qc-rule', rule.id)} className="library-rule"><div className="rule-drag">⠿</div><div><strong>{rule.name}</strong><p>{rule.instruction}</p>{rule.reference && <small className="reference-chip">▧ {rule.reference.name}</small>}</div><div className="rule-actions"><button type="button" className="icon-button" aria-label={`Attach a reference to ${rule.name}`} onClick={() => referenceInputs.current[rule.id]?.click()}>▧</button><button type="button" className="icon-button" aria-label={`Add ${rule.name} to work`} onClick={() => addRuleToWork(rule)}>＋</button></div><input ref={element => { referenceInputs.current[rule.id] = element }} className="hidden-input" type="file" accept="image/png,image/jpeg" onChange={event => attachReference(rule.id, event.target.files?.[0])}/></article>)}</div><p className="library-footnote">References travel with the rule and are only used when that rule runs.</p></aside>
      <section className="work-panel panel" onDragOver={event => event.preventDefault()} onDrop={handleRuleDrop}><div className="work-toolbar"><div><p className="step">02 / WORK</p><h2>Inspection plan</h2></div><span className="view-tag">{workRules.length ? 'Ready to run' : 'Add a rule'}</span></div><div className="drop-rules"><div className="drop-heading"><span>◎</span><div><strong>Rules for this work</strong><p>Drop from the Project library or add work-specific instructions.</p></div></div>{workRules.length ? <div className="work-rules">{workRules.map((rule, index) => <article className="work-rule" key={rule.id}><span className="rule-number">{index + 1}</span><div><div className="work-rule-title"><strong>{rule.name}</strong><span>{rule.source === 'project' ? 'PROJECT' : 'ONE-OFF'}</span></div><p>{rule.instruction}</p>{rule.reference && <small className="reference-chip">▧ {rule.reference.name}</small>}</div><button type="button" className="remove-rule" aria-label={`Remove ${rule.name}`} onClick={() => removeWorkRule(rule.id)}>×</button></article>)}</div> : <p className="empty-rules">Drag a rule here to start building the inspection plan.</p>}</div>{workRules.length > 0 && <div className="inspection-graph" aria-label="Inspection execution graph"><span className="graph-node">Artwork</span><span className="graph-arrow">→</span><div className="graph-rules">{workRules.map(rule => <span className="graph-node rule" key={rule.id}>{rule.name}</span>)}</div><span className="graph-arrow">→</span><span className="graph-node">Results</span></div>}<div className="ad-hoc-rule"><label htmlFor="work-instruction">Add instructions for this work</label><textarea id="work-instruction" rows={3} placeholder="e.g. The seafood image must look fresh. Do not use orange. CTA must say Book Now." value={draftInstruction} onChange={event => setDraftInstruction(event.target.value)}/><div><p>We’ll split this into independent rules before the check runs.</p><button type="button" disabled={!draftInstruction.trim()} onClick={extractRules}>Extract rules</button></div></div><div className="run-area"><label className="artwork-input"><input type="file" accept="image/png,image/jpeg" onChange={(event: ChangeEvent<HTMLInputElement>) => selectArtwork(event.target.files?.[0])}/><span>↥</span><div><strong>{file?.name ?? 'Choose final artwork'}</strong><small>{file ? `${formatBytes(file.size)} · ${specimen ? `${specimen.widthPx} × ${specimen.heightPx}` : 'reading dimensions…'}` : 'PNG, JPG or JPEG'}</small></div></label><button className="run-button" type="button" disabled={!file || !workRules.length || qcInProgress} onClick={runQc} aria-busy={qcInProgress}><span>{qcInProgress ? '◌' : '✦'}</span>{qcInProgress ? 'Inspecting…' : 'Run AI QC'}</button></div></section>
      <aside className="result-panel panel"><div className="result-header"><div><p className="step">03 / RESULTS</p><h2>{job?.status === 'EVALUATED' ? verdict === 'PASS' ? 'Ready for review' : 'Needs attention' : 'Rule-by-rule results'}</h2></div><div className={`overall-badge ${verdict.toLowerCase()}`}>{verdict}</div></div><p className="result-summary">{job?.status === 'ANALYZING' ? `Running ${workRules.length} independent checks…` : job?.status === 'EVALUATED' ? 'Each result remains traceable to the rule that produced it.' : 'Results will appear separately for every rule in this work.'}</p><ResultGroup title="AI visual QC" count={`${visualChecks.length || workRules.length} rules`} checks={visualChecks}/><div className="timestamp">{job?.status === 'EVALUATED' ? `QC run · ${new Intl.DateTimeFormat(undefined, { dateStyle: 'medium', timeStyle: 'short' }).format(new Date())}` : ''}</div></aside>
    </section>
    <section className="artwork-preview panel"><div><p className="step">ARTWORK</p><h2>Preview</h2></div><div className="artwork-stage">{preview ? <img src={preview} alt="Uploaded artwork preview"/> : <div className="empty-artwork"><div className="checker-icon"><i/><i/><i/><i/></div><p>Your final artwork will appear here</p></div>}</div></section>
    {job?.status === 'EVALUATED' && <section className="findings-area"><div className="findings-heading"><div><p className="step">FINDINGS</p><h2>What needs attention</h2></div><button className="text-button" onClick={() => setJob(null)}>Clear run</button></div><div className="findings-list">{findings.length ? findings.map((finding, index) => <article className="finding fail" key={`${finding.defectType}-${index}`}><div className="finding-meta"><span>{finding.ruleName}</span><span>{finding.severity}</span></div><h3>{labelFor(finding.defectType)}</h3><p>{finding.message}</p></article>) : <article className="finding pass"><div className="finding-meta"><span>Complete</span><span>PASS</span></div><h3>No issues found</h3><p>All selected rules completed without an issue that needs action.</p></article>}</div></section>}
  </main>
}

function ResultGroup({ title, count, checks }: { title: string; count: string; checks: Check[] }) { return <div className="result-group"><div className="group-title"><span>{title}</span><small>{count}</small></div><div className="checks">{checks.length ? checks.map(check => <article className={`check-row ${check.status}`} key={check.title}><span className="status-icon">{check.status === 'pass' ? '✓' : check.status === 'fail' ? '×' : '!'}</span><div><strong>{check.title}</strong><p>{check.detail}</p></div><span className="check-status">{check.status.toUpperCase()}</span></article>) : <p className="empty-checks">Run this work to see one result box per selected rule.</p>}</div></div> }
async function uploadImage(file: File): Promise<Specimen> { const presign = await fetch(`${INTAKE_API}/api/v1/uploads:presign`, { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ filename: file.name, contentType: file.type, sizeBytes: file.size }) }); if (!presign.ok) throw new Error(await errorMessage(presign)); const { uploadUrl } = await presign.json() as { uploadUrl: string }; const upload = await fetch(uploadUrl, { method: 'PUT', headers: { 'Content-Type': file.type }, body: file }); if (!upload.ok) throw new Error(await errorMessage(upload)); return upload.json() as Promise<Specimen> }
async function errorMessage(response: Response) { const payload = await response.json().catch(() => ({})) as { error?: string; code?: string; requestId?: string; technicalMessage?: string }; return [payload.error ?? `Request failed (${response.status})`, payload.technicalMessage && `Technical: ${payload.technicalMessage}`, payload.code && `Code: ${payload.code}`, payload.requestId && `Request ID: ${payload.requestId}`].filter(Boolean).join('\n') }
function ruleFor(rule: Rule) { return { id: rule.id, name: rule.name, ruleType: 'VISUAL_AI_QC', enabled: rule.enabled, severity: 'MAJOR', blocking: false, params: { categories: VISUAL_CHECKS, instruction: rule.instruction, referenceObjectKey: rule.referenceObjectKey ?? '' } } }
function buildVisualChecks(job: Job, workRules: Rule[]): Check[] { if (job.status !== 'EVALUATED') return []; return Object.values(job.tasks).map(task => { const rule = workRules.find(item => item.id === task.ruleId); const title = task.ruleName ?? rule?.name ?? labelFor(task.ruleType); if (task.state !== 'DONE') return { title, status: 'warning' as const, detail: task.state === 'SKIPPED' ? 'This rule was skipped.' : 'This checker was unavailable or incomplete.' }; const findings = task.findings ?? []; if (!findings.length) return { title, status: 'pass' as const, detail: 'No issues reported for this rule.' }; const highest = findings.some(finding => finding.severity !== 'MINOR') ? 'fail' as const : 'warning' as const; return { title, status: highest, detail: findings[0].message } }) }
function formatBytes(bytes: number) { return bytes < 1024 * 1024 ? `${Math.max(1, Math.round(bytes / 1024))} KB` : `${(bytes / 1024 / 1024).toFixed(2)} MB` }
function formatRelativeDate(value: string) { const date = new Date(value); return Number.isNaN(date.valueOf()) ? 'recently' : new Intl.DateTimeFormat(undefined, { day: 'numeric', month: 'short' }).format(date) }
function labelFor(value: string) { return value.replaceAll('_', ' ').replace(/\b\w/g, letter => letter.toUpperCase()) }

createRoot(document.getElementById('root')!).render(<App />)
