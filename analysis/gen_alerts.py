"""Generate six months of SYNTHETIC incident and alert history.

Nothing here comes from a real company. The data is generated from a
fixed seed so every run produces the same files, and its shape is chosen
to look like a real on-call history:

* incidents with a root cause, a service, a start and a resolve time
  (time to resolve is log-normal: most are quick, a few drag on);
* alerts that fire for those incidents, several rules per incident, and
  the same rule re-firing ("flapping") while the incident is open;
* noise: alerts that fire with no incident at all (a disk-usage rule
  that trips on log rotation, cert-expiry warnings, CPU blips, ...).

The analysis never reads the ground truth columns (cause on alerts,
incident_id on alerts); they are written to alerts_truth.csv so the tests
can check that the SQL recovered them.

    .venv/bin/python analysis/gen_alerts.py            # writes analysis/data/
"""

from __future__ import annotations

import csv
import math
import random
from dataclasses import dataclass
from datetime import datetime, timedelta, timezone
from pathlib import Path

SEED = 20261125
START = datetime(2026, 5, 1, tzinfo=timezone.utc)
END = datetime(2026, 11, 1, tzinfo=timezone.utc)  # six months
OUT = Path(__file__).parent / "data"


@dataclass(frozen=True)
class Cause:
    name: str
    per_month: float  # incidents per month
    mttr_median_min: float  # log-normal median of time to resolve
    mttr_sigma: float
    services: tuple[str, ...]
    rules: tuple[str, ...]  # rules that fire while this incident is open
    refire_every_min: float  # mean gap between re-fires of an open alert
    severity: str


# The planted answer: connection-pool exhaustion is not the most frequent
# cause, and its rules are not the noisiest, but it has the largest total
# customer impact (count x duration). Ranking by raw alert volume points
# at disk instead; that is the trap the analysis has to avoid.
# @learn:alert-causes
CAUSES = [
    Cause("db_pool_exhaustion", 6.0, 55, 0.6, ("inventory",), ("SearchP99High", "InventoryDBPoolSaturated", "GatewayErrorRate"), 6, "sev2"),
    Cause("dependency_timeout", 8.0, 18, 0.7, ("quote", "risk"), ("QuoteLatencyHigh", "HoldErrorRate"), 9, "sev3"),
    Cause("deploy_rollback", 5.0, 22, 0.5, ("search", "gateway", "inventory"), ("GatewayErrorRate", "DeployHealthCheckFailed"), 12, "sev2"),
    Cause("slow_query", 3.5, 40, 0.6, ("inventory",), ("InventoryQueryLatencyHigh", "SearchP99High"), 10, "sev3"),
    Cause("config_error", 2.0, 35, 0.7, ("gateway", "search", "profile"), ("GatewayErrorRate",), 15, "sev3"),
    Cause("disk_full", 1.2, 70, 0.5, ("inventory",), ("DiskUsageHigh", "InventoryQueryLatencyHigh"), 8, "sev3"),
    Cause("memory_leak_oom", 1.5, 25, 0.5, ("search", "profile"), ("PodRestarts", "SearchP99High"), 7, "sev3"),
    Cause("cert_expiry", 0.4, 95, 0.4, ("gateway",), ("TLSHandshakeErrors", "GatewayErrorRate"), 5, "sev1"),
]
# @learn:end alert-causes

# Alerts with no incident behind them: (rule, service, per day, mean
# duration in minutes, re-fires per firing).
NOISE = [
    ("DiskUsageHigh", "inventory", 3.0, 12, 3),  # log rotation pushes the disk over 85% for minutes
    ("CPUHigh", "search", 2.0, 6, 1),
    ("SearchP99High", "gateway", 0.6, 3, 1),  # short blips that resolve before anyone looks
    ("PodRestarts", "profile", 0.5, 2, 1),
    ("QuoteLatencyHigh", "quote", 0.4, 4, 1),
]
CERT_WARNING_DAYS = 30  # CertExpiringSoon fires once a day for 30 days before each renewal


def lognormal_minutes(rng: random.Random, median: float, sigma: float) -> float:
    return median * math.exp(rng.gauss(0, sigma))


def poisson_times(rng: random.Random, per_day: float):
    """Event times of a Poisson process over [START, END)."""
    t = START
    while True:
        t += timedelta(days=rng.expovariate(per_day))
        if t >= END:
            return
        yield t


def generate(seed: int = SEED):
    rng = random.Random(seed)
    incidents, alerts = [], []

    def fire(rule, service, sev, start, end, refire_every, incident_id, cause):
        """One alert firing plus its re-fires while the condition holds."""
        t = start
        while t < end:
            dur = min(end - t, timedelta(minutes=max(1.0, rng.expovariate(1 / max(refire_every * 0.6, 1)))))
            alerts.append(
                dict(
                    fired_at=t,
                    resolved_at=t + dur,
                    rule=rule,
                    service=service,
                    severity=sev,
                    fingerprint=f"{rule}/{service}",
                    _incident_id=incident_id,
                    _cause=cause,
                )
            )
            t += dur + timedelta(minutes=rng.expovariate(1 / refire_every))

    for c in CAUSES:
        for start in poisson_times(rng, c.per_month * 12 / 365):
            iid = f"INC-{len(incidents) + 1:04d}"
            mttr = lognormal_minutes(rng, c.mttr_median_min, c.mttr_sigma)
            end = start + timedelta(minutes=mttr)
            svc = rng.choice(c.services)
            incidents.append(dict(incident_id=iid, cause=c.name, service=svc, severity=c.severity, started_at=start, resolved_at=end))
            for rule in c.rules:
                if rule != c.rules[0] and rng.random() < 0.25:
                    continue  # secondary rules do not always trip
                lag = timedelta(minutes=rng.uniform(0.5, 6))  # detection lag
                # the alert is evaluated on the service that shows the symptom
                alert_svc = "gateway" if rule in ("SearchP99High", "GatewayErrorRate") and rng.random() < 0.7 else svc
                fire(rule, alert_svc, c.severity, start + lag, end, c.refire_every_min, iid, c.name)

    for rule, svc, per_day, mean_min, refires in NOISE:
        for start in poisson_times(rng, per_day):
            end = start + timedelta(minutes=lognormal_minutes(rng, mean_min, 0.5))
            fire(rule, svc, "warning", start, end, mean_min / max(refires, 1) + 4, "", "")

    # certificate renewals: a warning every day before each renewal
    renewals = [START + timedelta(days=d) for d in (40, 130)]
    for r in renewals:
        for d in range(CERT_WARNING_DAYS, 0, -1):
            t = r - timedelta(days=d) + timedelta(hours=9, minutes=rng.uniform(0, 5))
            fire("CertExpiringSoon", "gateway", "warning", t, t + timedelta(minutes=1), 999, "", "")

    alerts.sort(key=lambda a: a["fired_at"])
    for i, a in enumerate(alerts, 1):
        a["alert_id"] = f"AL-{i:06d}"
    incidents.sort(key=lambda x: x["started_at"])
    return incidents, alerts


def fmt(t: datetime) -> str:
    return t.strftime("%Y-%m-%d %H:%M:%S")


def write(incidents, alerts, out: Path = OUT) -> None:
    out.mkdir(parents=True, exist_ok=True)
    with open(out / "incidents.csv", "w", newline="") as f:
        w = csv.writer(f)
        w.writerow(["incident_id", "cause", "service", "severity", "started_at", "resolved_at"])
        for x in incidents:
            w.writerow([x["incident_id"], x["cause"], x["service"], x["severity"], fmt(x["started_at"]), fmt(x["resolved_at"])])
    with open(out / "alerts.csv", "w", newline="") as f:
        w = csv.writer(f)
        w.writerow(["alert_id", "fired_at", "resolved_at", "rule", "service", "severity", "fingerprint"])
        for a in alerts:
            w.writerow([a["alert_id"], fmt(a["fired_at"]), fmt(a["resolved_at"]), a["rule"], a["service"], a["severity"], a["fingerprint"]])
    with open(out / "alerts_truth.csv", "w", newline="") as f:
        w = csv.writer(f)
        w.writerow(["alert_id", "incident_id", "cause"])
        for a in alerts:
            w.writerow([a["alert_id"], a["_incident_id"], a["_cause"]])


if __name__ == "__main__":
    inc, al = generate()
    write(inc, al)
    noise = sum(1 for a in al if not a["_incident_id"])
    print(f"{len(inc)} incidents, {len(al)} alerts ({noise} with no incident) -> {OUT}")
