import com.sun.net.httpserver.HttpExchange;
import com.sun.net.httpserver.HttpServer;

import java.io.IOException;
import java.io.OutputStream;
import java.net.InetSocketAddress;
import java.util.ArrayList;
import java.util.List;
import java.util.concurrent.Executors;
import java.util.concurrent.atomic.AtomicBoolean;
import java.util.concurrent.atomic.AtomicLong;

public class WorkloadApp {

    // ---------- shared state ----------
    private static final AtomicBoolean active = new AtomicBoolean(false);
    private static final AtomicLong iterations = new AtomicLong(0);

    // Calibrated iteration counts so loops consume the right wall-clock time
    private static long calmIterations;   // ~5 ms worth
    private static long busyIterations;   // ~50 ms worth
    private static final long calmAllocMB = 1;
    private static final long busyAllocMB = 32;

    // Objects held during /allocate endpoint calls (released after 10 s)
    private static final List<byte[]> held = new ArrayList<>();

    // ---------- main ----------
    public static void main(String[] args) throws Exception {
        calibrate();

        Thread workload = new Thread(WorkloadApp::workloadLoop, "workload");
        workload.setDaemon(true);
        workload.start();

        HttpServer server = HttpServer.create(new InetSocketAddress(8080), 0);
        // Register specific paths before the catch-all "/"
        server.createContext("/health",     ex -> handleHealth(ex));
        server.createContext("/activate",   ex -> handleActivate(ex));
        server.createContext("/deactivate", ex -> handleDeactivate(ex));
        server.createContext("/status",     ex -> handleStatus(ex));
        server.createContext("/allocate",   ex -> handleAllocate(ex));
        server.createContext("/",           ex -> handleRoot(ex));
        server.setExecutor(Executors.newFixedThreadPool(20));
        server.start();
        System.out.println("WorkloadApp listening on port 8080");
    }

    // ---------- calibration ----------
    private static void calibrate() {
        // Run math loop for 200 ms, count iterations, derive per-ms rate
        long count = 0;
        long end = System.currentTimeMillis() + 200;
        while (System.currentTimeMillis() < end) {
            burnCpu(10_000);
            count += 10_000;
        }
        long perMs = Math.max(1, count / 200);
        calmIterations = perMs * 5;   //  5 ms
        busyIterations = perMs * 50;  // 50 ms
        System.out.printf("Calibrated: %d iter/ms -> calm=%d busy=%d%n",
                perMs, calmIterations, busyIterations);
    }

    // ---------- workload loop ----------
    private static void workloadLoop() {
        while (true) {
            boolean isBusy = active.get();
            long iters   = isBusy ? busyIterations : calmIterations;
            long allocMB = isBusy ? busyAllocMB    : calmAllocMB;

            burnCpu(iters);
            iterations.incrementAndGet();
            allocateAndRelease(allocMB);

            try {
                // stay close to 1-second cadence accounting for work time
                Thread.sleep(isBusy ? 950 : 995);
            } catch (InterruptedException e) {
                Thread.currentThread().interrupt();
                return;
            }
        }
    }

    // ---------- CPU work (produces interesting flamegraph stacks) ----------
    private static double burnCpu(long n) {
        double acc = 0;
        for (long i = 0; i < n; i++) {
            acc += Math.sin(i) * Math.sqrt(i + 1);
        }
        return acc; // value used to prevent dead-code elimination
    }

    // ---------- heap work (transient — released immediately) ----------
    private static void allocateAndRelease(long mb) {
        List<Object> local = new ArrayList<>();
        long bytesLeft = mb * 1024 * 1024;

        // ~50% byte arrays
        local.add(new byte[(int) Math.min(bytesLeft / 2, Integer.MAX_VALUE)]);
        bytesLeft -= bytesLeft / 2;

        // ~25% String array
        int strings = (int) Math.min(bytesLeft / 64, 10_000);
        String[] sa = new String[strings];
        for (int i = 0; i < strings; i++) sa[i] = "workload-string-" + i;
        local.add(sa);

        // ~25% linked nodes
        Node head = null;
        int nodes = (int) Math.min(bytesLeft / 32, 5_000);
        for (int i = 0; i < nodes; i++) head = new Node(i, head);
        local.add(head);

        // local goes out of scope here — GC collects
    }

    // ---------- /allocate endpoint: holds memory for 10 s ----------
    private static void holdAllocate(long mb) {
        byte[] block = new byte[(int) Math.min(mb * 1024 * 1024, Integer.MAX_VALUE)];
        synchronized (held) { held.add(block); }
        Thread releaser = new Thread(() -> {
            try { Thread.sleep(10_000); } catch (InterruptedException e) { Thread.currentThread().interrupt(); }
            synchronized (held) { held.remove(block); }
        });
        releaser.setDaemon(true);
        releaser.start();
    }

    // ---------- linked list node (visible in heap dumps) ----------
    private static class Node {
        final int value;
        final Node next;
        Node(int value, Node next) { this.value = value; this.next = next; }
    }

    // ---------- HTTP handlers ----------
    private static void handleRoot(HttpExchange ex) throws IOException {
        // Small CPU burst per request — creates thread contention under load
        burnCpu(1_000);
        respond(ex, 200, "{\"status\":\"ok\"}");
    }

    private static void handleHealth(HttpExchange ex) throws IOException {
        respond(ex, 200, "OK");
    }

    private static void handleActivate(HttpExchange ex) throws IOException {
        if (!"POST".equalsIgnoreCase(ex.getRequestMethod())) {
            respond(ex, 405, "Method Not Allowed"); return;
        }
        active.set(true);
        respond(ex, 200, "{\"active\":true}");
    }

    private static void handleDeactivate(HttpExchange ex) throws IOException {
        if (!"POST".equalsIgnoreCase(ex.getRequestMethod())) {
            respond(ex, 405, "Method Not Allowed"); return;
        }
        active.set(false);
        respond(ex, 200, "{\"active\":false}");
    }

    private static void handleStatus(HttpExchange ex) throws IOException {
        String body = String.format("{\"active\":%b,\"iterations\":%d}",
                active.get(), iterations.get());
        respond(ex, 200, body);
    }

    private static void handleAllocate(HttpExchange ex) throws IOException {
        if (!"POST".equalsIgnoreCase(ex.getRequestMethod())) {
            respond(ex, 405, "Method Not Allowed"); return;
        }
        long mb = 32;
        String query = ex.getRequestURI().getQuery();
        if (query != null) {
            for (String part : query.split("&")) {
                if (part.startsWith("mb=")) {
                    try { mb = Long.parseLong(part.substring(3)); } catch (NumberFormatException ignored) {}
                }
            }
        }
        holdAllocate(mb);
        respond(ex, 200, "{\"allocated_mb\":" + mb + "}");
    }

    private static void respond(HttpExchange ex, int code, String body) throws IOException {
        byte[] bytes = body.getBytes("UTF-8");
        ex.getResponseHeaders().set("Content-Type", "application/json");
        ex.sendResponseHeaders(code, bytes.length);
        try (OutputStream os = ex.getResponseBody()) { os.write(bytes); }
    }
}
