---
name: solution-design
description: "Architecture consultation and solution brainstorming for the engineering team. Use for design decisions, comparing technical options, trade-off analysis, feasibility checks, choosing a fix approach, and deciding whether work needs design before implementation."
---

# Solution Design

A structured way to turn a technical question into a decision with honest trade-offs. It does not implement anything.

## When to Use

- `techosin`: deciding whether a request needs design work before implementation and scoping it, designing a solution or choosing between approaches, reviewing a `/planning` plan for architectural fit, and choosing between fix approaches for a blocker.

## Principles

- **YAGNI, KISS, DRY.** The simplest design that meets the acceptance criteria wins. Reject flexibility nobody asked for.
- **Brutal honesty.** If an idea is unrealistic, over-engineered, or risky, say so plainly and say why.
- **Evidence over memory.** Separate verified facts (with their source) from assumptions. Never invent repository structure, modules, or conventions.
- **Hard gate: no implementation inside this skill.** The output is a decision and a design. Implementation starts only after it, through Orca.

## Process

### 1. Frame the problem

Write down, one sentence each, before proposing anything:

1. **Expected output**: what artifact or behavior must exist at the end.
2. **Acceptance criteria**: how anyone can verify it is done.
3. **Scope boundary**: what is explicitly out of scope.
4. **Constraints**: stack, interfaces that must not break, deadlines, cost.
5. **Touchpoints**: existing components, files, or services it touches.

If any item is vague and changes the answer, do not guess. Return the exact question to the requester, with the options you see.

### 2. Gather context

- Read every file the requester attached first.
- Search internal knowledge: `memory_search`, `vault_search`, and `knowledge_graph_search` when available.
- Research outside sources with `web_search` and `web_fetch`. Prefer official docs, release notes, and issue trackers over blog posts.
- Facts that need the codebase come from Orca (a check terminal or a planning run), never from memory. Anything not checked yet is marked as an assumption.

### 3. Check the scope

If the request covers three or more independent concerns, stop and propose a split into sub-problems with a build order. Design each one separately.

### 4. Look at it from four angles

- **Systems design**: boundaries, interfaces, data flow, component responsibilities.
- **Technology**: stack and pattern fit with what already exists.
- **Scalability and reliability**: load, failure modes, recovery, operability.
- **Risk**: dependencies, migration, security, reversibility, cost.

### 5. Compare options

Present two or three viable options, never more. For each: how it works, pros, cons, effort, risk, and what would make it the wrong choice. Pick one and give the deciding reason. When every option is bad, say so.

### 6. Write the result

Short answers go straight into the reply. Anything longer goes into a file in your current workspace (for example `design.md`) with `write_file`, revised with `edit`, and is listed by name in the result.

## Output Template

```
Decision: <recommended option, one line>
Why: <deciding reason>

Problem and constraints
Options considered (2-3) with pros, cons, effort, risk
Recommended design: components, interfaces and contracts, data model and flow
Implementation guidance for the coding agent (what, not how to run it)
Risks, migration and rollback notes
Assumptions to verify in the codebase
Open questions for the requester
Sources
```

For a plan review, use instead:

```
Verdict: consistent | consistent with changes | inconsistent
Findings: severity (critical|high|medium|low) — area — problem — required change
"No findings" when there are none
```

## Anti-Patterns

| Thought | Reality |
|---|---|
| "Too simple to need a design" | Simple work wastes the most time on unexamined assumptions. A short design takes a minute. |
| "I already know the answer" | Then writing the trade-off down is quick. Do it. |
| "Let me add an option for later" | YAGNI. Add it when someone asks. |
| "The codebase probably does X" | You cannot see it. Mark it as an assumption or ask. |
