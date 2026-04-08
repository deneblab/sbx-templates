---
name: issue
description: "Intelligent issues"
---

# Intelligent issues

I'll create complete feature structures based on your project patterns, with full continuity across sessions.

Arguments: `$ARGUMENTS` - issue ID (IssuesId)

## Session Intelligence

I'll maintain issue progress across sessions:

**Session Files (in current project directory):**
- `.agents/issues/{IssuesId}/plan.md` - issue plan and component list
- `.agents/issues/{IssuesId}/state.json` - Created files and progress

**IMPORTANT:** Session files are stored in a `.agents/issues` folder in your current project root

**Auto-Detection:**
- If session exists: Resume incomplete issue
- If no session: Create new issue plan
- Commands: `resume`, `status`, `new`

## Phase 1: Pattern Discovery

**MANDATORY FIRST STEPS:**
1. Check if `.agents/issues` directory exists in current working directory
2. If directory exists, check for session files:
   - Look for `.agents/issues/{IssuesId}/issue.md`
   - Look for `.agents/issues/{IssuesId}/state.json`
   - Look for `.agents/issues/{IssuesId}/plan.md`
   - If found, resume from existing session
3. If no directory or session exists:
   - Analyze project patterns
   - Create issues plan
   - Initialize progress tracking
4. Show issues preview before creating

**Note:** Always look for session files in the current project's `.agents/issues/{IssuesId}/` folder, not `../../../.agents/issues/{IssuesId}/`` or absolute paths

I'll discover your project patterns:

**Pattern Analysis:**
- File organization structure
- Naming conventions
- Testing patterns
- Import/export styles
- Documentation standards

**Smart Detection:**
- Find similar features already implemented
- Identify architectural patterns
- Detect testing frameworks
- Understand build configuration

## Phase 2: Issue Planning

Based on patterns, I'll create a Issue plan:

**Component Structure:**
- Main feature files
- Test files
- Documentation
- Configuration updates
- Integration points

I'll write this plan to `.agents/issues/{IssuesId}/plan.md` with:
- Each file to create
- Template patterns to follow
- Integration requirements
- Creation order

## Phase 3: Intelligent Generation

I'll generate files matching your patterns:

**Pattern Matching:**
- Use your file naming style
- Follow your directory structure
- Match your code conventions
- Apply your testing patterns

**Content Generation:**
- Boilerplate from existing code
- Imports matching your style
- Test structure from your patterns
- Documentation in your format

## Phase 4: Incremental Creation

I'll create files systematically:

**Execution Process:**
1. Create directory structure
2. Generate each component file
3. Add appropriate tests
4. Update integration points
5. Track each creation in state

**Progress Tracking:**
- Mark each file created in plan
- Update state with file paths
- Create meaningful commits

## Phase 5: Integration

After scaffolding:
- Update route configurations
- Add to module exports
- Update build configuration
- Verify everything connects

## Context Continuity

**Session Resume:**
When you return and run `/issue-open` or `/issue-open resume`:
- Load existing plan and progress
- Show what was already created
- Continue from last component
- Maintain pattern consistency

**Progress Example:**
```
RESUMING SCAFFOLDING
├── Feature: UserDashboard
├── Created: 5 of 8 files
├── Last: components/UserStats.tsx
└── Next: tests/UserStats.test.tsx

Continuing scaffolding...
```

## Practical Examples

**Start Scaffolding:**
```
/issue ROBE-16          # Create issue that has ID: ROBE-16 (Id from Youtrack)
/issue GH-632        # Create issue that has ID: GH-632 (Number - 632 - from GitHub)
/issue my-very-importent-task       # Create issue that has ID: my-very-importent-task
```

**Session Control:**
```
/issue resume    # Continue existing issue
/issue status    # Check what's been created
/issue new       # Start fresh issue
```

## Safety Guarantees

**Protection Measures:**
- Preview before creation
- Incremental file generation
- Pattern validation
- Integration verification

**Important:** I will NEVER:
- Overwrite existing files
- Break existing imports
- Add AI attribution
- Create without following patterns

## What I'll Actually Do

1. **Analyze deeply** - Understand your patterns
2. **Plan completely** - Map all components
3. **Generate intelligently** - Match your style
4. **Track precisely** - Perfect continuity
5. **Integrate seamlessly** - Connect everything

I'll maintain complete continuity between sessions, always resuming exactly where we left off with consistent pattern application.