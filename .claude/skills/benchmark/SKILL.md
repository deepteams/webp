---
name: benchmark
description: Run benchmark and save results
model: haiku
---

## Instructions
Run the benchmarks with a count of 10 in the benchmark folder and give me the summary.
Update README.md and benchmark/README.md

```
cd benchmark
go test -bench=. -benchmem -count=10 -run=^$ -timeout=30m
```
