# Migration from system-upgrade-controller to Argo Workflows

## Overview

Заменили `system-upgrade-controller` на прямое выполнение обновлений через Argo Workflows.

## Why?

**system-upgrade-controller = обёртка над теми же privileged Job'ами:**
- ✅ Те же скрипты
- ✅ Те же права (privileged + hostPath)
- ✅ То же chroot выполнение
- ❌ Дополнительный компонент
- ❌ Дополнительный CRD (Plan)
- ❌ Нет rollback

**Argo Workflows даёт больше гибкости:**
- ✅ Единая платформа для всей автоматизации
- ✅ Лучшая видимость (Argo UI)
- ✅ Переиспользование template для разных сценариев
- ✅ Встроенные pre/post hooks
- ✅ Suspend для manual approval
- ✅ Concurrency control через parallelism

## Architecture

### Before:
```
Calendar EventSource → Sensor → Argo Workflow (apply Plan)
                                      ↓
                           system-upgrade-controller → Jobs
```

### After:
```
Calendar EventSource → Sensor → Argo Workflow (privileged pods)
```

## Components

### WorkflowTemplate: node-upgrade

Файл: `manifests/argo-workflows/workflow-template-node-upgrade.yaml`

**Параметры:**
- `node-role`: `control-plane` или `worker`
- `concurrency`: количество нод параллельно (default: 1)
- `upgrade-script`: bash скрипт для выполнения

**Workflow:**
1. Получить список нод с нужной ролью
2. Для каждой ноды последовательно:
   - Создать privileged pod на этой ноде
   - Смонтировать host filesystem (`/` → `/host`)
   - Выполнить upgrade script через `chroot /host`
   - Дождаться готовности ноды после reboot

### Sensor: system-upgrade

Файл: `manifests/argo-events/sensor-system-upgrade.yaml`

**Triggers:**
1. **node-upgrade-control-plane** - автоматически обновляет control plane ноды
2. **node-upgrade-workers** - создаёт Workflow с `suspend: true` для ручного approve

## Usage

### Автоматическое обновление (еженедельно)

Calendar EventSource запускает оба Workflow:
- Control plane ноды обновляются автоматически
- Worker ноды требуют ручного approve в Argo UI

### Ручное обновление control plane

```bash
argo submit --from workflowtemplate/node-upgrade \
  --parameter node-role=control-plane \
  --namespace argo-events
```

### Ручное обновление workers

```bash
argo submit --from workflowtemplate/node-upgrade \
  --parameter node-role=worker \
  --namespace argo-events
```

### Кастомный скрипт

```bash
argo submit --from workflowtemplate/node-upgrade \
  --parameter node-role=control-plane \
  --parameter upgrade-script='#!/bin/bash
set -e
apt-get update
apt-get install --yes htop
' \
  --namespace argo-events
```

## Migration Steps

### 1. Deploy new WorkflowTemplate

Уже в репо: `manifests/argo-workflows/workflow-template-node-upgrade.yaml`

ArgoCD автоматически задеплоит при синхронизации.

### 2. Update Sensor

Уже обновлён: `manifests/argo-events/sensor-system-upgrade.yaml`

Новые triggers используют `node-upgrade` вместо `system-upgrade`.

### 3. Test manually

```bash
# Тестовый запуск (без reboot)
argo submit --from workflowtemplate/node-upgrade \
  --parameter node-role=control-plane \
  --parameter upgrade-script='#!/bin/bash
echo "Test run on $(hostname)"
apt-get update
echo "Success!"
' \
  --namespace argo-events \
  --watch
```

### 4. Remove system-upgrade-controller (optional)

**Если всё работает, можно удалить:**

```bash
# Удалить ArgoCD Application
kubectl delete application system-upgrade-controller --namespace argocd

# Удалить Plans
kubectl delete plans --all --namespace system-upgrade

# Удалить namespace (после завершения всех Jobs)
kubectl delete namespace system-upgrade
```

**Файлы для удаления из репо:**
- `argocd/infra/system-upgrade-controller.yaml`
- `plans/system-plan.yaml`

## Rollback

Если нужно вернуться к system-upgrade-controller:

```bash
# Откатить изменения в git
git revert HEAD

# Восстановить ArgoCD Application
kubectl apply --filename argocd/infra/system-upgrade-controller.yaml
```

## Notes

- WorkflowTemplate использует `privileged: true` и `hostPath: /`
- Reboot через `nsenter --target 1 --mount --uts --ipc --net --pid -- reboot`
- ServiceAccount `argo-workflow` должен иметь права на создание privileged pods
- Worker upgrades требуют manual approval (`suspend: true`)
- Можно добавить drain/cordon раскомментировав соответствующие steps

## Security

**Те же риски, что и system-upgrade-controller:**
- Privileged containers
- Host filesystem access
- Root execution via chroot

**Дополнительная безопасность:**
- Manual approval для workers через `suspend: true`
- Видимость всех операций в Argo UI
- Audit trail всех выполненных команд
