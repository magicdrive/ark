package com.shop.report;

public class LedgerReport {
    long total(Ledger ledger) {
        return ledger.entry000(1L) + ledger.entry129(2L);
    }
}
