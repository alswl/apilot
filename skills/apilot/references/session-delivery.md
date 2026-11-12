# Connect an existing task to the current agent

Use this workflow when a restarted or newly opened agent should receive an
existing project's scheduled messages. Runtime adaptation is part of the work;
the user should not need to provide a particular command name.

## 1. Establish the current receiver

Inspect the agent's actual tool capabilities and trusted current-session
metadata. Read the relevant runtime or integration documentation and available
interface help. Establish:

| Fact | Evidence needed |
| --- | --- |
| Current receiver | The current conversation's identity, confirmed by the supported runtime interface |
| Delivery address | An address/handle bound to that receiver, including its scope and lifetime |
| Receive mode | Native push, a supported subscription, explicit inbox read, or unavailable |
| Readiness | Runtime acknowledgement of the receiver/binding/subscription |
| Receipt proof | How a received event and its challenge can be observed in this conversation |

Do not select a runtime by recognizing executable names, aliases, a messaging
command, terminal type, or a familiar environment variable. A launcher is opaque.
An environment-provided ID alone does not prove its endpoint is reachable or
bound to the current conversation. A terminal input API alone does not prove
that text will become an agent turn. Use supported interfaces, not internal
transport files, keystroke injection or an unrelated resumed session.

Reuse an available runtime integration when it supplies these capabilities.
If a project-specific adapter is needed, use the runtime's documented receiving
API and keep that adaptation local to this task/project. Do not introduce a
particular messaging dependency just to fit this recipe. When no receiving API
exists, explain the documented recovery prerequisite and stop the dependent
reconnect work; do not invent endpoints, flags or automatic reception.

## 2. Resolve the task and the owner of its target

Locate the exact existing task by ID/name and working directory. Inspect its
executable, literal argv, environment **key names**, description, revision,
schedule and recent runs. Read only the relevant declared adapter/configuration
needed to understand where it sends messages.

Find the authoritative delivery target: it may be an argument, an explicit
environment override, or a configuration used by the task's adapter. Do not do
a global string replacement of session IDs or change every similarly named task.
If a shared configuration would redirect other tasks, find a supported scoped
override instead or surface that concrete scope issue.

Keep a private, redacted before-state sufficient to compare routing, schedule,
enabled state, timeout, cwd and business argv. Store no credentials in public
artifacts. For a task mutation use its inspected revision; after a conflict,
reread and reconsider both the target and the fields another actor changed.
For adapter configuration, use its supported update mechanism and check for
concurrent edits. Do not delete/recreate a task to avoid a conflict.

A direct message can preflight the receiver, but is not yet proof that apilot's
existing task uses it. Once readiness is established, update only the selected
task's target through the appropriate owner. Read back the task/configuration
and confirm the new receiver and all unrelated fields.
For redacted environment values, a key list cannot prove the target's value;
confirm the effective target using the adapter's supported introspection and
the handshake, without reading unrelated credentials.

## 3. Exercise the real delivery path

Generate a fresh unpredictable challenge, for example `apilot-probe-` plus a
random UUID. Record when verification began and the expected receiver.

Use the adapter's documented probe/correlation mechanism to carry the challenge
through the selected task's delivery path. Do not assume an arbitrary environment
key will be forwarded, replace business content with a test command, or invent
an `apilot task run --message` flag. If the adapter has no probe facility, use a
fresh Run/event ID and receiver-side correlation the adapter actually exposes.
If no reliable correlation is possible, report that gap rather than claim a
verified handshake. Any temporary probe setting must be scoped, reversible and
removed after verification without overwriting concurrent changes.

Trigger that task **once** with `apilot task run TASK_ID --json` and retain the
returned Run ID. Inspect that Run's status and relevant delivery logs. An
independent direct-send test to the new receiver does not substitute for this
scheduler-to-receiver leg. If the task's existing work has additional effects,
choose its documented diagnostic mode or surface the specific effect before
triggering; a reconnect request is not permission for unrelated business work.

The receiving agent must observe the matching event in this current conversation
and acknowledge its challenge/event ID. Accept only evidence tied to this
verification attempt and receiver, not an old receipt, shared nickname, sender
stdout, queue acceptance, or receipt by another conversation. If the adapter
waits for the agent's acknowledgement before completing, receive/ack first and
then inspect the final Run; do not wait for completion while blocking its ACK.
Use the runtime's receipt acknowledgement interface: `apilot event ack` only
marks an apilot attention event seen and is not an agent-message ACK.

For a runtime that delivers only after the current turn ends, state “probe sent;
awaiting receipt” and yield. Continue on the actual inbound event. A manual
inbox read can verify explicit reception, but does not establish automatic
wake-up. Test while idle before claiming idle delivery. Use bounded checks;
leave the probe pending rather than creating watcher/poll loops indefinitely.

## 4. Close the handshake

Record the task ID/revision, receiver identity, receive mode, target change,
Run ID/final state, challenge or correlation ID, and actual receipt/ACK evidence.
Declare end-to-end success only when the task used the intended route, the Run
completed successfully, and the current session received and acknowledged the
matching event. If one leg is still pending, report that leg separately.

After a restart, re-establish receiver identity and readiness and repeat the
handshake. A previously successful route is evidence about that earlier session,
not this new one. Preserve the existing task's purpose and scheduling throughout.

Example request:

> $apilot，把这个项目已有的提醒任务接到当前会话，并验证一次真实投递。

The same request works with any agent runtime that exposes the necessary
receiving interface; adapt using its capabilities and documentation.
