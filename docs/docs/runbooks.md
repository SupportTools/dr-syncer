---
sidebar_position: 7
---

# Operational Runbooks

This guide provides step-by-step operational runbooks for disaster recovery operations using DR-Syncer. Each runbook includes checklists, verification steps, and rollback procedures.

## Overview

DR-Syncer supports three operational modes:

| Mode | Purpose | Use Case |
|------|---------|----------|
| **Stage** | Prepare DR environment | Initial setup, regular sync updates |
| **Cutover** | Activate DR, shut down production | Disaster event, planned migration |
| **Failback** | Return to production | Recovery after disaster resolved |

### Prerequisites

Before executing any runbook:

- [ ] DR-Syncer CLI installed (`dr-syncer-cli`)
- [ ] Access to source cluster kubeconfig
- [ ] Access to destination cluster kubeconfig
- [ ] `pv-migrate` installed (if migrating PVC data)
- [ ] Appropriate RBAC permissions on both clusters

---

## Runbook 1: Pre-Disaster Preparation (Stage)

**Purpose**: Set up and maintain DR environment readiness
**Frequency**: Initial setup, then scheduled (daily/weekly)
**Risk Level**: Low
**Estimated Duration**: 15-60 minutes (depending on data volume)

### Pre-Stage Checklist

```bash
# 1. Verify connectivity to source cluster
kubectl --kubeconfig ~/.kube/prod cluster-info

# 2. Verify connectivity to destination cluster
kubectl --kubeconfig ~/.kube/dr cluster-info

# 3. Ensure destination namespace exists
kubectl --kubeconfig ~/.kube/dr get namespace production-dr || \
  kubectl --kubeconfig ~/.kube/dr create namespace production-dr

# 4. Check source namespace resources
kubectl --kubeconfig ~/.kube/prod get all -n production

# 5. Verify PVC status in source (if syncing PVC data)
kubectl --kubeconfig ~/.kube/prod get pvc -n production
```

### Stage Execution

```bash
# Basic staging (without PVC data)
dr-syncer-cli --mode Stage \
  --source-kubeconfig ~/.kube/prod \
  --dest-kubeconfig ~/.kube/dr \
  --source-namespace production \
  --dest-namespace production-dr

# With PVC data migration
dr-syncer-cli --mode Stage \
  --source-kubeconfig ~/.kube/prod \
  --dest-kubeconfig ~/.kube/dr \
  --source-namespace production \
  --dest-namespace production-dr \
  --migrate-pvc-data=true

# With specific resource filtering
dr-syncer-cli --mode Stage \
  --source-kubeconfig ~/.kube/prod \
  --dest-kubeconfig ~/.kube/dr \
  --source-namespace production \
  --dest-namespace production-dr \
  --resource-types=deployments,services,configmaps,secrets
```

### Post-Stage Verification

```bash
# 1. Verify resources were created in destination
kubectl --kubeconfig ~/.kube/dr get all -n production-dr

# 2. Confirm deployments are scaled to 0 (dormant)
kubectl --kubeconfig ~/.kube/dr get deployments -n production-dr -o wide
# Expected: All READY columns should show 0/0

# 3. Verify StatefulSets are scaled to 0
kubectl --kubeconfig ~/.kube/dr get statefulsets -n production-dr -o wide

# 4. Confirm PVCs exist (if applicable)
kubectl --kubeconfig ~/.kube/dr get pvc -n production-dr

# 5. Check for any sync events/errors
kubectl --kubeconfig ~/.kube/dr get events -n production-dr --sort-by='.lastTimestamp' | tail -20
```

### Optional: Test DR Activation

To validate DR environment without affecting production:

```bash
# Scale up a single deployment temporarily
kubectl --kubeconfig ~/.kube/dr scale deployment/my-app -n production-dr --replicas=1

# Verify pod starts successfully
kubectl --kubeconfig ~/.kube/dr get pods -n production-dr -l app=my-app

# Scale back down
kubectl --kubeconfig ~/.kube/dr scale deployment/my-app -n production-dr --replicas=0
```

### Troubleshooting Stage Failures

| Symptom | Possible Cause | Resolution |
|---------|---------------|------------|
| "namespace not found" | Destination namespace missing | Create namespace manually |
| "forbidden" errors | RBAC permissions | Check service account permissions |
| PVC migration fails | pv-migrate not installed | Install pv-migrate tool |
| Timeout errors | Large data volumes | Increase timeout, use `--pv-migrate-flags` |

---

## Runbook 2: DR Cutover

**Purpose**: Activate DR environment and shut down production
**Risk Level**: High
**Estimated Duration**: 5-30 minutes (plus PVC migration time)

### Cutover Decision Tree

```
Is this a disaster event?
├── YES: Production is down/inaccessible
│   └── Proceed with Cutover immediately
│       └── Skip source verification steps if unreachable
│
└── NO: Planned migration/maintenance
    └── Follow full pre-cutover checklist
        ├── Notify stakeholders
        ├── Schedule maintenance window
        └── Verify all prerequisites
```

### Pre-Cutover Checklist

```bash
# 1. Document current production state
kubectl --kubeconfig ~/.kube/prod get deployments -n production -o wide
kubectl --kubeconfig ~/.kube/prod get statefulsets -n production -o wide
kubectl --kubeconfig ~/.kube/prod get pods -n production

# 2. Save replica counts (automatic during cutover, but good to document)
kubectl --kubeconfig ~/.kube/prod get deployments -n production \
  -o jsonpath='{range .items[*]}{.metadata.name}{"\t"}{.spec.replicas}{"\n"}{end}'

# 3. Verify DR environment is staged and current
kubectl --kubeconfig ~/.kube/dr get deployments -n production-dr

# 4. Check PVC sync status (if applicable)
kubectl --kubeconfig ~/.kube/prod get pvc -n production \
  -o jsonpath='{range .items[*]}{.metadata.name}{"\t"}{.metadata.annotations.dr-syncer\.io/sync-status}{"\n"}{end}'

# 5. Verify no active transactions (application-specific)
# Example: Check database connections, queue depths, etc.
```

### Communication Steps

Before executing cutover:

1. **Notify stakeholders** (operations, development, business)
2. **Update status page** (if applicable)
3. **Alert on-call personnel**
4. **Document start time**: `date -u +"%Y-%m-%dT%H:%M:%SZ"`

### Cutover Execution

```bash
# Standard cutover (without PVC data)
dr-syncer-cli --mode Cutover \
  --source-kubeconfig ~/.kube/prod \
  --dest-kubeconfig ~/.kube/dr \
  --source-namespace production \
  --dest-namespace production-dr

# With final PVC data sync
dr-syncer-cli --mode Cutover \
  --source-kubeconfig ~/.kube/prod \
  --dest-kubeconfig ~/.kube/dr \
  --source-namespace production \
  --dest-namespace production-dr \
  --migrate-pvc-data=true

# With extended timeout for large PVCs
dr-syncer-cli --mode Cutover \
  --source-kubeconfig ~/.kube/prod \
  --dest-kubeconfig ~/.kube/dr \
  --source-namespace production \
  --dest-namespace production-dr \
  --migrate-pvc-data=true \
  --pv-migrate-flags="--strategy rsync --lbsvc-timeout 60m"
```

### Post-Cutover Verification

```bash
# 1. Verify source deployments scaled to 0
kubectl --kubeconfig ~/.kube/prod get deployments -n production -o wide
# Expected: All READY columns should show 0/0

# 2. Verify destination deployments scaled up
kubectl --kubeconfig ~/.kube/dr get deployments -n production-dr -o wide
# Expected: READY columns should match original replica counts

# 3. Verify pods are running in destination
kubectl --kubeconfig ~/.kube/dr get pods -n production-dr
# Expected: All pods in Running state

# 4. Check original replica annotation (for failback)
kubectl --kubeconfig ~/.kube/prod get deployments -n production \
  -o jsonpath='{range .items[*]}{.metadata.name}{"\t"}{.metadata.annotations.dr-syncer\.io/original-replicas}{"\n"}{end}'

# 5. Test service endpoints
kubectl --kubeconfig ~/.kube/dr get svc -n production-dr
kubectl --kubeconfig ~/.kube/dr get endpoints -n production-dr
```

### External Dependency Updates

After cutover verification:

| Component | Action | Verification |
|-----------|--------|--------------|
| **DNS** | Update A/CNAME records to DR cluster IPs | `dig <service-domain>` |
| **Load Balancer** | Update backend targets | Health check status |
| **API Gateway** | Update upstream endpoints | Test API calls |
| **Monitoring** | Update targets, silence prod alerts | Dashboard reflects DR |
| **CDN** | Purge cache, update origin | Cache invalidation |

### Cutover Rollback (If Needed)

If cutover fails and production is still accessible:

```bash
# Use Failback mode to restore production
dr-syncer-cli --mode Failback \
  --source-kubeconfig ~/.kube/prod \
  --dest-kubeconfig ~/.kube/dr \
  --source-namespace production \
  --dest-namespace production-dr

# Revert external dependencies to production endpoints
```

---

## Runbook 3: Failback to Production

**Purpose**: Return workloads from DR to original production environment
**Risk Level**: Medium-High
**Estimated Duration**: 10-60 minutes (plus PVC migration time)

### Pre-Failback Assessment

```bash
# 1. Verify production cluster is healthy and accessible
kubectl --kubeconfig ~/.kube/prod cluster-info
kubectl --kubeconfig ~/.kube/prod get nodes

# 2. Document current DR state
kubectl --kubeconfig ~/.kube/dr get deployments -n production-dr -o wide
kubectl --kubeconfig ~/.kube/dr get pods -n production-dr

# 3. Verify original replica annotations exist
kubectl --kubeconfig ~/.kube/prod get deployments -n production \
  -o jsonpath='{range .items[*]}{.metadata.name}{"\t"}{.metadata.annotations.dr-syncer\.io/original-replicas}{"\n"}{end}'

# 4. Check PVC data freshness (if applicable)
kubectl --kubeconfig ~/.kube/dr get pvc -n production-dr \
  -o jsonpath='{range .items[*]}{.metadata.name}{"\t"}{.metadata.annotations.dr-syncer\.io/last-sync}{"\n"}{end}'
```

### PVC Reverse Sync Decision

```
Has data changed in DR during the disaster period?
├── YES: Application wrote data while running in DR
│   └── Use --reverse-migrate-pvc-data=true
│       └── This syncs DR data back to production
│
└── NO: DR was read-only or no persistent data
    └── Skip PVC reverse migration
        └── Production PVCs retain pre-disaster data
```

### Communication Steps

Before executing failback:

1. **Notify stakeholders** of planned failback window
2. **Verify root cause of disaster is resolved**
3. **Plan for brief service interruption during transition**
4. **Document start time**: `date -u +"%Y-%m-%dT%H:%M:%SZ"`

### Failback Execution

```bash
# Standard failback (without PVC reverse sync)
dr-syncer-cli --mode Failback \
  --source-kubeconfig ~/.kube/prod \
  --dest-kubeconfig ~/.kube/dr \
  --source-namespace production \
  --dest-namespace production-dr

# With PVC data reverse sync (DR → Production)
dr-syncer-cli --mode Failback \
  --source-kubeconfig ~/.kube/prod \
  --dest-kubeconfig ~/.kube/dr \
  --source-namespace production \
  --dest-namespace production-dr \
  --reverse-migrate-pvc-data=true
```

### Post-Failback Verification

```bash
# 1. Verify DR deployments scaled to 0
kubectl --kubeconfig ~/.kube/dr get deployments -n production-dr -o wide
# Expected: All READY columns should show 0/0

# 2. Verify production deployments restored
kubectl --kubeconfig ~/.kube/dr get deployments -n production -o wide
# Expected: READY columns should match original replica counts

# 3. Verify pods are running in production
kubectl --kubeconfig ~/.kube/prod get pods -n production
# Expected: All pods in Running state

# 4. Test service endpoints
kubectl --kubeconfig ~/.kube/prod get endpoints -n production

# 5. Verify application health
# (Application-specific health checks)
```

### External Dependency Restoration

After failback verification:

| Component | Action | Verification |
|-----------|--------|--------------|
| **DNS** | Restore original A/CNAME records | `dig <service-domain>` |
| **Load Balancer** | Restore original backend targets | Health check status |
| **API Gateway** | Restore original upstream endpoints | Test API calls |
| **Monitoring** | Restore production targets, re-enable alerts | Dashboard reflects prod |

### Post-Failback Maintenance

```bash
# 1. Re-stage DR environment for future readiness
dr-syncer-cli --mode Stage \
  --source-kubeconfig ~/.kube/prod \
  --dest-kubeconfig ~/.kube/dr \
  --source-namespace production \
  --dest-namespace production-dr \
  --migrate-pvc-data=true

# 2. Verify DR is ready for next event
kubectl --kubeconfig ~/.kube/dr get deployments -n production-dr -o wide

# 3. Document lessons learned
# - What triggered the disaster?
# - How long was recovery?
# - What could be improved?
```

---

## Runbook 4: Emergency Quick Reference

### Quick Commands

```bash
# Check cluster connectivity
kubectl --kubeconfig <config> cluster-info

# View all resources in namespace
kubectl --kubeconfig <config> get all -n <namespace>

# Check deployment replica status
kubectl --kubeconfig <config> get deployments -n <namespace> -o wide

# View recent events
kubectl --kubeconfig <config> get events -n <namespace> --sort-by='.lastTimestamp' | tail -20

# Check original replica annotations
kubectl get deploy -n <namespace> -o jsonpath='{range .items[*]}{.metadata.name}{"\t"}{.metadata.annotations.dr-syncer\.io/original-replicas}{"\n"}{end}'
```

### Emergency Contacts Template

| Role | Name | Contact | Escalation |
|------|------|---------|------------|
| Primary On-Call | | | |
| Secondary On-Call | | | |
| Infrastructure Lead | | | |
| Application Owner | | | |
| Security Team | | | |

### Rollback Decision Criteria

**Rollback from Cutover if:**
- DR pods fail to start within 10 minutes
- Critical services unavailable after cutover
- Data integrity issues detected
- Performance degradation exceeds acceptable thresholds

**Rollback from Failback if:**
- Production pods fail to start within 10 minutes
- PVC data sync fails or shows corruption
- Network connectivity issues to production cluster

---

## Appendix: Resource Types Synchronized

By default, DR-Syncer synchronizes these resource types:

| Resource | Synchronized | Notes |
|----------|--------------|-------|
| ConfigMaps | Yes | Excluding system configmaps |
| Secrets | Yes | Excluding service account tokens |
| Deployments | Yes | Scaled to 0 in Stage mode |
| StatefulSets | Yes | Scaled to 0 in Stage mode |
| DaemonSets | Yes | |
| Services | Yes | ClusterIP regenerated |
| Ingresses | Yes | |
| ServiceAccounts | Yes | |
| Roles | Yes | |
| RoleBindings | Yes | |
| PersistentVolumeClaims | Yes | Data sync optional |
| HorizontalPodAutoscalers | Yes | |
| NetworkPolicies | Yes | |

### Filtering Resources

```bash
# Include only specific types
--resource-types=deployments,services,configmaps

# Exclude specific types
--exclude-resource-types=networkpolicies,horizontalpodautoscalers

# Include custom resources
--include-custom-resources=true
```

---

## Related Documentation

- [Troubleshooting Guide](./troubleshooting.md) - Diagnosing and resolving issues
- [Features](./features.md) - Detailed feature documentation
- [CLI Usage](./cli-usage.md) - Complete CLI reference
- [CRD Reference](./crd-reference.md) - API reference for controller mode
