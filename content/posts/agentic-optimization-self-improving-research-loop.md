---
title: "Agentic Optimization: A Self-Improving AI Research Loop"
date: 2026-06-22
slug: agentic-optimization-self-improving-research-loop
description: "How an agentic optimization loop used AI agents, held-out BPB, and a research ledger to improve a byte-level model from 1.871 to 1.604 BPB."
tags: [ai, agents, optimization, research, machine-learning]
draft: false
summary: "A practical agentic optimization loop: agents proposed language-model experiments, a strict evaluator scored BPB, and a ledger made each round smarter."
---

I built an **agentic optimization** loop for language-model research.

AI agents proposed candidate models, a strict evaluator scored held-out bits per
byte, and a research ledger turned each result into a better next brief. Over
six iterations, the loop moved from a 1.871 BPB byte-GPT baseline to a replicated
1.604 BPB sparse MoE result.

The important part was not that an AI agent wrote a model file. It was that the
system kept improving its own research process: better rules, better prompts,
better rejection criteria, and better guesses about what to test next.

This is not recursive self-improving AGI. It is a bounded research loop for
language-model experiments, with a fixed compute budget, a held-out evaluation
set, and one boring metric that every candidate has to face.

| Question | Answer |
| --- | --- |
| What was optimized? | Byte-level language-model experiments. |
| Who proposed candidates? | AI agents working from a shared brief and ledger. |
| Who judged results? | A separate evaluator using held-out BPB. |
| What improved? | Best known BPB moved from 1.871 to 1.604. |
| What made it work? | Strict evaluation, equal budgets, and persistent memory. |

That boring metric is the point.

## What Is Agentic Optimization?

Agentic optimization is the use of AI agents inside a measured improvement loop:
generate candidates, run them under a fixed contract, score them with an
external evaluator, write down what happened, and use that memory to choose the
next search direction.

It sits near a long research lineage around search, learning, and
self-improving systems: Rich Sutton's ["Bitter Lesson"](http://www.incompleteideas.net/IncIdeas/BitterLesson.html),
Schmidhuber's [Godel Machine](https://people.idsia.ch/~juergen/goedelmachine.html),
Jeff Clune's [AI-GAs](https://arxiv.org/abs/1905.10985), and newer work such as
[The AI Scientist](https://arxiv.org/abs/2408.06292), [Darwin Godel Machine](https://arxiv.org/abs/2505.22954),
and DeepMind's [AlphaEvolve](https://deepmind.google/blog/alphaevolve-a-gemini-powered-coding-agent-for-designing-advanced-algorithms/).

My version is intentionally modest: can agents run a serious empirical loop for
improving a small byte-level language model?

The setup was simple:

- agents design candidate models
- each candidate trains under the same token budget
- an evaluator scores held-out bits per byte, or BPB
- lower BPB means better next-byte prediction
- results go into a research ledger
- the next iteration starts from the ledger, not from intuition

![Diagram of the agentic optimization loop](/static/img/agentic-optimization-loop.svg)

*The loop is intentionally simple: agents can be creative because the evaluator is strict.*

## The Rules That Made Agents Useful

The first lesson was that agents are not automatically good researchers. They
are energetic proposal generators. Without rules, they over-tune, chase noisy
wins, invent explanations too early, and spend compute on experiments that are
hard to compare.

So the real work was designing the game they were allowed to play.

| Rule | Why it mattered |
| --- | --- |
| The orchestrator owns evaluation. | Candidate code cannot choose the held-out set or grade itself. |
| Every candidate gets the same budget. | Training was fixed at 120 million tokens, making runs comparable. |
| Held-out BPB is the metric. | No custom success criteria, hand-picked examples, or self-reported wins. |
| The held-out data stays held out. | Training used a loader that excluded the evaluation slice. |
| Noise is measured first. | Five baseline seeds gave a 1.871 mean BPB with sigma 0.0317. |
| Small wins do not count. | A result had to clear the noise scale before becoming a real claim. |
| Every iteration updates the ledger. | Results, crashes, timeouts, surprises, and assumptions became memory. |

The ledger mattered more than I expected. It stopped the agents from
rediscovering the same lesson every round. Once those rules existed, the main
agent could keep working: read the ledger, choose pressure points, spawn focused
subagents, run candidates, score them, update the written rules, and repeat.

The human role changed from "pick the next model" to "design a loop where the
next model choice has to be earned."

## How the Research Loop Worked

Each candidate lived in its own directory with a manifest, code, and notes. After
training, it had to serve a small local API:

- health check
- log probabilities
- shutdown

The evaluator sent held-out bytes to the candidate and computed BPB itself. This
contract made very different ideas comparable: n-grams, compression-style
models, GRUs, LSTMs, transformers, Mamba-style attempts, RWKV-style attempts,
and sparse mixture-of-experts models all had to satisfy the same interface.

The experiment was small enough to run repeatedly, but strict enough to punish
storytelling. That combination is the useful middle ground: creativity happens
before evaluation, not during evaluation.

## Outcomes Over Six Iterations

The baseline was a small byte-level GPT: 6 layers, 384 dimensions, about 10.7
million parameters. It scored 1.871 BPB on the held-out set.

Then the agents started proposing alternatives.

![Line chart showing best BPB by iteration](/static/img/agentic-optimization-results.svg)

*Best known held-out BPB after each iteration. Lower is better.*

| Iteration | Best result | What happened |
| --- | ---: | --- |
| 0 | 1.871 | Baseline byte-GPT established the noise scale. |
| 1 | 1.879 | Classical and retrieval-heavy ideas did not beat the baseline. |
| 2 | 1.640 | A tuned modern transformer became the first major jump. |
| 3 | 1.638 | Dense transformer variants mostly tied the new plateau. |
| 4 | 1.629 | Training recipe changes helped, but only slightly. |
| 5 | 1.605 | Sparse MoE broke below the dense transformer floor. |
| 6 | 1.604 | A second full MoE run replicated the improvement. |

The biggest drop came from moving to a stronger modern transformer recipe. After
that, dense transformer changes became much harder. Deeper, narrower,
longer-context, and recipe-tuned variants clustered around the same region.

The next real improvement came from sparse mixture of experts. The best model,
`iter6-moe-steps`, reached 1.6037 BPB. A previous full MoE configuration had
reached 1.6052 BPB, close enough to look like a replicated pattern rather than a
lucky run.

A single run is a hint. Two independent configurations landing in the same
region below the dense floor is evidence.

## Interesting Findings

| Finding | Why it mattered |
| --- | --- |
| A strict evaluator beat a clever prompt. | Claims stopped mattering. A candidate either served log probabilities and improved held-out BPB, or it did not. |
| Agents needed calibrated context. | After each round, agents received the current champion, the noise estimate, the budget, and known failure modes. |
| Diversity was not automatically useful. | Classical, retrieval-heavy, and unstable sequence-model attempts narrowed the search even when they failed. |
| Dense transformer tuning hit a local floor. | Once the recipe reached roughly 1.64 BPB, many plausible improvements became sub-sigma. |
| Sparse capacity helped. | MoE added capacity without paying full dense cost on every token, and two full MoE runs landed below the dense floor. |

The loop improved because failure stayed visible. Crashes, NaNs, timeouts, and
weak scores were not embarrassing side notes. They became instructions for the
next round.

## How the Agents Improved Over Time

The useful thing was not that an agent wrote a good model once.

The useful thing was that the loop became harder to fool over time.

By later iterations, the system had a better playbook:

- compare against the current champion, not the original baseline
- treat sub-sigma changes as noise
- prefer experiments that test a specific hypothesis
- preserve failures because they teach the next prompt
- separate proposal generation from scoring
- only promote a candidate after the evaluator agrees

That is the practical version of self-improvement here. Not a model rewriting
its own source code into superintelligence, but a research process improving its
own instructions, memory, and search priorities.

## Why "Agentic Optimization" Fits

"AI agent" is too broad. "Automated research" is too grand. "Recursive
self-improvement" suggests something much stronger than what happened.

**Agentic optimization** is closer to the actual pattern:

- use agents to generate candidates
- use code to enforce the contract
- use data to decide winners
- use a ledger to carry learning forward
- repeat until the search stops paying rent

It is not magic. It is a disciplined optimization loop where language models
supply imagination and software supplies accountability.

That shape can apply beyond language-model experiments: prompts, retrieval
systems, data pipelines, compilers, model architectures, evaluation harnesses,
or any domain where you can define a strict score and make candidates cheap
enough to test.

## What I Would Improve Next

The next version should have a sealed test slice that is used less often, so the
research loop cannot gradually overfit the main held-out set. It should add a
small out-of-distribution canary, make failure analysis more structured, and
push agents to produce more differentiated hypotheses instead of variations on
the same favorite recipe.

Promotion should also be stricter:

1. one seed to reject obvious losers
2. multiple seeds for plausible challengers
3. independent replication before calling something a new direction

That keeps the system fast when ideas are bad and careful when ideas look good.

## Takeaway

Agents become more useful when they are placed inside a loop that remembers,
measures, and refuses to be impressed by prose.

For this experiment, agentic optimization moved a byte-level language model from
a 1.871 BPB baseline to a replicated 1.604 BPB MoE result. The result is
interesting, but the loop is the part I want to keep building.
