# Game Instructions

## Guess Who: Identity Under Fire

You are competing against other teams to identify hidden characters from a board of 64 anonymised candidates. Use the API to ask questions, narrow down the candidates, and make your guess — as fast as possible, with as few questions as possible.

---

## Getting Started

### 1. Register Your Team

```
POST /client/signup
{
  "teamName": "your-team-name",
  "password": "your-password"
}
```

### 2. Log In

```
POST /client/login
{
  "teamName": "your-team-name",
  "password": "your-password"
}
```

Save the `token` from the response — you'll need it for authenticated endpoints. Also note your team's `id`.

---

## Playing the Game

### 3. Start a Session

```
POST /sessions/start
X-Team-Id: <your-team-id>
```

You'll receive:
- A `sessionId` — use this for all subsequent requests
- A `board` — up to 64 candidates, each with a **fake ID** (e.g. `P01`, `P02`, …) and a subset of 20 traits
- An `encryptionKey` and `cipherType` — needed to decrypt encrypted answers

> **Note:** You can have up to 2 active sessions at a time. Each session targets a different character.

### 4. Get the Available Questions

```
GET /questions
```

Returns all 64 trait questions with their `questionId`, `traitKey`, and `type`. Use these to ask questions about the target character.

### 5. Ask Questions

```
POST /sessions/{sessionId}/ask
{
  "questionId": "T07"
}
```

The response tells you the target character's value for that trait:

```json
{
  "questionId": "T07",
  "traitKey": "hair_color",
  "answer": "brown",
  "status": ""
}
```

Use the answers to eliminate candidates from your board.

> **Watch out:** Some answers will be **encrypted**. When `status` is `"encrypted"`, the `answer` field contains base64-encoded ciphertext. Decrypt it using your session's `encryptionKey` and `cipherType`.

> **Tip:** The first question in a session is never encrypted.

### 6. Make Your Guess

Once you've narrowed it down, submit your guess using the candidate's **fake ID**:

```
POST /sessions/{sessionId}/guess
{
  "candidateId": "P12"
}
```

- **Correct:** You'll receive a score breakdown and the character's real ID
- **Incorrect:** You lose one guess (you have 3 total). Each wrong guess costs **−200 points**

### 7. Reveal (Last Resort)

If you're stuck, you can reveal the target character — but only after asking **at least 5 questions**:

```
POST /sessions/{sessionId}/reveal
```

This costs **−500 points** and ends the session.

---

## Scoring

| Event | Points |
|-------|--------|
| Correct guess (base) | +1000 |
| Time bonus (≤60s) | +500 |
| Time bonus (≤120s) | +400 |
| Time bonus (≤180s) | +300 |
| Time bonus (≤300s) | +200 |
| Time bonus (≤600s) | +100 |
| Question bonus (1–3 questions) | +300 |
| Question bonus (4–6 questions) | +200 |
| Question bonus (7–10 questions) | +100 |
| Wrong guess | −200 |
| Reveal | −500 |

Solve fast, ask few questions, and handle errors gracefully to maximise your score.

---

## Milestones

Milestones are bonus points awarded automatically for reaching certain achievements. Each milestone is awarded once per team.

| Milestone | Points | Condition |
|-----------|--------|-----------|
| M1 — First Steps | +1000 | Start your first session |
| M2 — First Question | +1000 | Ask your first question |
| M3 — Getting Warmer | +1000 | Ask your third question |
| M4 — Got One! | +1000 | Get your first correct guess |
| S1 — Sharp Shooter | +2000 | Solve in 3 or fewer questions |
| S2 — Speed Demon | +2000 | Achieve the fastest solve time |
| S3 — Chaos Survivor | +2000 | Ask a question during a chaos event |

---

## Chaos Events

During the game, organisers may trigger **chaos events** that cause the `/ask` endpoint to return errors. Your automation must handle these gracefully:

- Implement **retry logic with exponential backoff**
- Chaos failures are tracked and applied as a **reliability penalty** to your score
- Successfully asking a question during chaos earns the **S3 milestone** (+2000 pts)

---

## Tips

- **Decrypt encrypted answers** — ~40% of answers after the first will be encrypted. Your session's `encryptionKey` and `cipherType` tell you how to decrypt them.
- **Use trait types** — questions return a `type` (`boolean`, `enum`, `numeric`). Use this to understand what values to expect.
- **Each candidate has only 20 of 64 traits** — you can only ask questions that are in a candidate's trait subset. Plan your questions carefully.
- **Traits are session-wide** — once you ask a question, the answer applies to the target character for the whole session. You don't need to ask the same question twice.
- **Watch the leaderboard** — the home page shows real-time updates as teams solve characters.
- **Reset your board** — if you've solved all 64 characters, use `POST /client/team/reset` (with your JWT) to reset your solved list and start fresh.

---

## Quick Reference

| Action | Endpoint |
|--------|----------|
| Sign up | `POST /client/signup` |
| Log in | `POST /client/login` |
| Start session | `POST /sessions/start` (X-Team-Id header) |
| Get questions | `GET /questions` |
| Ask question | `POST /sessions/{id}/ask` |
| Submit guess | `POST /sessions/{id}/guess` |
| Reveal target | `POST /sessions/{id}/reveal` |
| Check status | `GET /sessions/{id}/status` |
| View leaderboard | `GET /client/game/leaderboard` |
| View master board | `GET /client/game/master-board` |
| View team progress | `GET /client/team/progress` (JWT required) |
| Reset board | `POST /client/team/reset` (JWT required) |