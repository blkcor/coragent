# S2.4 Effect Analyzer: Pattern-based Command Classification

**Status:** pending
**Prerequisite:** [S2.1 accepted](01-sandbox-interface.md) (type definitions available)

## Goal

实现规则引擎，对 command 进行三分类（safe-readonly / workspace-mutation / dangerous）。分类是确定性的、hard-coded 的，dangerous 规则不可被模型覆盖。为 Policy Engine 提供决策依据。

## Deliverables

- `internal/policy/effect.go`：`EffectAnalyzer` 类型
  - `Classify(cmd string, args []string, grants Grants) EffectClassification`
  - `EffectClassification` 枚举：`EffectSafe`、`EffectWorkspace`、`EffectDangerous`
- `internal/policy/rules.go`：规则表定义
  - 三类规则各为 `[]CommandPattern`
  - `CommandPattern` 包含：`Command string`（可执行文件名）、`ArgPatterns []string`（简单 glob）、`Classification`
  - 规则按优先级匹配：dangerous > workspace > safe
- `internal/policy/rules_test.go`：规则表测试
  - 每个分类的代表性命令匹配
  - 边界情况：`rm -rf` dangerous，`git rm` workspace
  - 优先级：`sudo ls` → dangerous（sudo 优先于 ls）
  - 参数模式匹配正确性

### 规则表

**Safe Read-only**（无副作用，始终允许）：

```
ls, pwd, cat, head, tail, less
grep, rg, fd, find, eza
git status, git diff, git log, git show
git branch, git tag
wc, sort, uniq, cut, tr
go doc, go vet, cargo check
which, type, command -v
```

**Workspace Mutation**（修改 workspace，首次审批后续放行）：

```
git add, git commit, git rm, git mv
git checkout (file paths), git restore, git reset
go build, go test, go run, go mod tidy, go fmt
cargo build, cargo test, cargo fmt
npm install, npm test, npm run, npx
pnpm install, pnpm test, yarn install, yarn test
pytest, pytest --update
make, just
touch, mkdir, cp, mv (within workspace)
sed -i, sd (in-place edit)
echo > (redirect within workspace)
```

**Dangerous**（高风险，始终审批或拒绝）：

```
rm, rm -rf, rmdir
sudo, su, doas
chmod, chown, chgrp
curl ... | sh, wget ... | bash
ssh, scp, sftp, rsync (remote)
docker, docker-compose, kubectl, helm
git push, git push --force, git push --delete
systemctl, service, launchctl, supervisorctl
mount, umount, dd, mkfs
eval, exec, source
pip install (global), npm install -g, gem install
iptables, ufw, firewall-cmd
kill, pkill, killall
```

## Acceptance

- [ ] `EffectAnalyzer.Classify` 对所有 safe 示例返回 `EffectSafe`
- [ ] `EffectAnalyzer.Classify` 对所有 workspace 示例返回 `EffectWorkspace`
- [ ] `EffectAnalyzer.Classify` 对所有 dangerous 示例返回 `EffectDangerous`
- [ ] 优先级：dangerous 规则优先于其他（`sudo cat` → dangerous，不是 safe）
- [ ] 未匹配的命令默认归类为 `EffectWorkspace`（保守策略：不确定就当 mutation 处理）
- [ ] 规则表易于维护：新增规则只需在 `rules.go` 中添加一行
- [ ] 离线测试覆盖：所有规则的正向匹配 + 优先级冲突 + 未知命令 fallback

## Evidence

Retain test output under `artifacts/m2/s2/2.4/`.

## Design notes

- 规则匹配使用 **exact binary name** + **prefix arg pattern**。不解析 shell 语法（pipeline、redirect 由 command tool 在 prepare 阶段处理）。
- Pipeline（`cmd1 | cmd2`）按最高风险分类。`ls | grep foo` → safe（两者都是 safe）。`cat file | ssh host` → dangerous（ssh 触发 dangerous）。
- Redirect 的目标路径需要检查：`echo foo > /etc/passwd` → dangerous（路径在 workspace 外）。`echo foo > ./bar` → workspace。
- 不可覆盖规则：dangerous 匹配后模型不能通过 tool call hint 覆盖。safe/workspace 边界可以加 future override field，但 M2 不做。
- 规则表是 Go 编译期常量。不在文件中配置——避免模型或用户篡改。将来可以加 `--allow-dangerous` flag 但默认行为是硬编码的。
