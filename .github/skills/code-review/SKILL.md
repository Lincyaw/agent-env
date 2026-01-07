---
name: code-review
description: Professional code review for anti-patterns, extensibility, architectural soundness, and code quality. Use when users request code review, ask to check for anti-patterns, need architectural validation, want to ensure extensibility, or request analysis of code quality. Apply to tasks involving code auditing, refactoring recommendations, design pattern validation, or ensuring every line of code has purpose and meaning.
license: Complete terms in LICENSE.txt
---

# Code Review Skill

Professional code review focused on anti-patterns, extensibility, architectural soundness, and ensuring every line of code has clear purpose.

## Review Principles

1. **Every line must justify its existence** - No dead code, unnecessary abstractions, or premature optimization
2. **Extensibility first** - Code should be designed to accommodate future changes with minimal modification
3. **Architectural soundness** - Overall structure must be robust, maintainable, and follow proven patterns
4. **Anti-pattern detection** - Identify and flag common mistakes that reduce code quality

## Review Process

### 1. Initial Analysis

Start by understanding the codebase structure:

- Identify architectural layers and their boundaries
- Map dependencies between components
- Understand the domain model and business logic flow
- Review configuration and deployment patterns

### 2. Anti-Pattern Detection

Review code for common anti-patterns. See [anti-patterns.md](references/anti-patterns.md) for comprehensive list organized by language and category.

Key categories:
- **Structural**: God objects, tight coupling, circular dependencies
- **Behavioral**: Callback hell, error swallowing, magic numbers
- **Architectural**: Missing layers, broken abstractions, leaky abstractions
- **Performance**: N+1 queries, premature optimization, resource leaks

### 3. Extensibility Analysis

Evaluate how well the code can accommodate change:

- **Open/Closed Principle**: Extensions without modification
- **Dependency Injection**: Loose coupling through abstractions
- **Interface Segregation**: Focused, purposeful interfaces
- **Configuration Management**: Externalized, environment-aware settings

See [extensibility-patterns.md](references/extensibility-patterns.md) for detailed patterns and examples.

### 4. Architecture Validation

Assess overall architectural health:

- **Layer Separation**: Clear boundaries between presentation, business, data layers
- **Dependency Direction**: Dependencies flow inward toward domain core
- **Component Cohesion**: Related functionality grouped logically
- **Error Handling Strategy**: Consistent, comprehensive approach
- **Observability**: Logging, metrics, tracing properly integrated

See [architecture-checks.md](references/architecture-checks.md) for detailed validation criteria.

### 5. Code Purposefulness

Ensure every line has clear justification:

- **Dead Code**: Unused functions, variables, imports
- **Redundant Logic**: Duplicated code that should be extracted
- **Unnecessary Abstraction**: Over-engineering for current requirements
- **Missing Documentation**: Complex logic without explanation
- **Unclear Naming**: Variables/functions with ambiguous names

## Review Output Format

Structure review findings as follows:

### Critical Issues (Must Fix)

Issues that break architectural principles or introduce serious anti-patterns.

**Format:**
```
[CRITICAL] <File>:<Line> - <Issue Title>

Pattern: <Anti-pattern name>
Problem: <Why this is critical>
Impact: <Consequences if not fixed>
Fix: <Specific recommended solution>
```

### Major Issues (Should Fix)

Issues that reduce extensibility or violate important design principles.

**Format:**
```
[MAJOR] <File>:<Line> - <Issue Title>

Problem: <What's wrong>
Impact: <Why this matters>
Recommendation: <How to improve>
```

### Minor Issues (Consider Fixing)

Code that works but could be improved for clarity or maintainability.

**Format:**
```
[MINOR] <File>:<Line> - <Issue Title>

Current: <What the code does now>
Suggestion: <Potential improvement>
Benefit: <Why this would be better>
```

### Positive Observations

Highlight well-designed code to reinforce good patterns.

**Format:**
```
[GOOD] <File>:<Line> - <What's done well>

Pattern: <Design pattern or principle>
Why: <Why this is good practice>
```

## Language-Specific Considerations

### Go

- Proper error handling (not ignoring errors)
- Interface usage for testability and extensibility
- Context propagation for cancellation
- Goroutine lifecycle management
- Proper resource cleanup with defer

### Python

- Type hints for all public APIs
- Pydantic models for data validation (not raw dicts)
- Proper exception handling (not catching bare Exception)
- Avoiding global mutable state
- Async/await patterns for I/O operations

### General

- Consistent naming conventions
- Proper abstraction levels
- Clear separation of concerns
- Comprehensive error handling
- Appropriate test coverage

## Review Workflow

1. **Scan for critical issues first** - Security, data loss, crashes
2. **Check architectural patterns** - Layer violations, dependency issues
3. **Review for extensibility** - How easy to add features
4. **Evaluate code purpose** - Every line justified
5. **Provide actionable feedback** - Specific, not generic
6. **Prioritize findings** - Critical > Major > Minor
7. **Suggest concrete fixes** - Not just "this is bad"

## When to Reference Detailed Guides

- **Unfamiliar anti-pattern**: Read [anti-patterns.md](references/anti-patterns.md) for specific pattern details
- **Extensibility questions**: Read [extensibility-patterns.md](references/extensibility-patterns.md) for design patterns
- **Architecture validation**: Read [architecture-checks.md](references/architecture-checks.md) for comprehensive criteria
- **Language-specific issues**: Read relevant section in anti-patterns guide

## Red Flags

Immediately flag these issues:

- **Security vulnerabilities**: SQL injection, XSS, insecure secrets
- **Data loss potential**: Missing transactions, race conditions
- **Resource leaks**: Unclosed files, connections, goroutines
- **Broken error handling**: Swallowed errors, panic in production
- **Missing critical validation**: User input without sanitization
- **Tight coupling**: Changes in one place require many other changes
- **Unclear ownership**: Ambiguous responsibility for data/logic
