package demo

import (
	"time"

	"github.com/shhac/prtea/internal/github"
)

const demoUsername = "demo-user"

// Fictional users
var (
	userAlice = github.User{Login: "alice", AvatarURL: "https://github.com/alice.png"}
	userBob   = github.User{Login: "bob", AvatarURL: "https://github.com/bob.png"}
	userCarol = github.User{Login: "carol", AvatarURL: "https://github.com/carol.png"}
	userDave  = github.User{Login: "dave", AvatarURL: "https://github.com/dave.png"}
	userEve   = github.User{Login: "eve", AvatarURL: "https://github.com/eve.png"}
	userFrank = github.User{Login: "frank", AvatarURL: "https://github.com/frank.png"}
	userDemo  = github.User{Login: demoUsername, AvatarURL: "https://github.com/demo-user.png"}
)

// Fictional repos
var (
	repoGateway   = github.Repo{Owner: "acme", Name: "gateway", FullName: "acme/gateway"}
	repoDashboard = github.Repo{Owner: "acme", Name: "dashboard", FullName: "acme/dashboard"}
	repoNexus     = github.Repo{Owner: "acme", Name: "nexus", FullName: "acme/nexus"}
	repoPlatform  = github.Repo{Owner: "acme", Name: "platform", FullName: "acme/platform"}
	repoAllocator = github.Repo{Owner: "acme", Name: "allocator", FullName: "acme/allocator"}
	repoPipeline  = github.Repo{Owner: "acme", Name: "pipeline", FullName: "acme/pipeline"}
)

var baseTime = time.Date(2026, 2, 15, 10, 0, 0, 0, time.UTC)

// -- PR Items (list view) --

// prsForReview are PRs authored by others that demo-user is reviewing.
var prsForReview = []github.PRItem{
	{
		ID: 1001, Number: 101, Title: "Add rate limiting middleware",
		HTMLURL: "https://github.com/acme/gateway/pull/101",
		Repo: repoGateway, Author: userAlice,
		Labels:    []github.Label{{Name: "enhancement", Color: "a2eeef"}, {Name: "api", Color: "d4c5f9"}},
		CreatedAt: baseTime.Add(-48 * time.Hour),
		Additions: 45, Deletions: 0, ChangedFiles: 1,
	},
	{
		ID: 1002, Number: 202, Title: "Migrate to React Server Components",
		HTMLURL: "https://github.com/acme/dashboard/pull/202",
		Repo: repoDashboard, Author: userBob,
		Labels:    []github.Label{{Name: "refactor", Color: "e4e669"}, {Name: "breaking", Color: "d73a4a"}},
		Draft:     true,
		CreatedAt: baseTime.Add(-24 * time.Hour),
		Additions: 38, Deletions: 25, ChangedFiles: 1,
	},
	{
		ID: 1003, Number: 303, Title: "Implement async connection pool",
		HTMLURL: "https://github.com/acme/nexus/pull/303",
		Repo: repoNexus, Author: userCarol,
		Labels:    []github.Label{{Name: "feature", Color: "0075ca"}},
		CreatedAt: baseTime.Add(-2 * time.Hour),
		Additions: 52, Deletions: 0, ChangedFiles: 1,
	},
	{
		ID: 1004, Number: 404, Title: "Add dependency injection for services",
		HTMLURL: "https://github.com/acme/platform/pull/404",
		Repo: repoPlatform, Author: userDave,
		Labels:    []github.Label{{Name: "refactor", Color: "e4e669"}, {Name: "services", Color: "bfd4f2"}},
		CreatedAt: baseTime.Add(-72 * time.Hour),
		Additions: 32, Deletions: 18, ChangedFiles: 1,
	},
}

// myPRs are PRs authored by demo-user.
var myPRs = []github.PRItem{
	{
		ID: 1005, Number: 505, Title: "Optimize memory allocator",
		HTMLURL: "https://github.com/acme/allocator/pull/505",
		Repo: repoAllocator, Author: userDemo,
		Labels:    []github.Label{{Name: "performance", Color: "f9d0c4"}},
		CreatedAt: baseTime.Add(-30 * time.Minute),
		Additions: 25, Deletions: 10, ChangedFiles: 1,
	},
	{
		ID: 1006, Number: 606, Title: "Add type hints to data pipeline",
		HTMLURL: "https://github.com/acme/pipeline/pull/606",
		Repo: repoPipeline, Author: userDemo,
		Labels:    []github.Label{{Name: "typing", Color: "c5def5"}, {Name: "cleanup", Color: "fef2c0"}},
		CreatedAt: baseTime.Add(-96 * time.Hour),
		Additions: 35, Deletions: 22, ChangedFiles: 1,
	},
}

// -- PR Details --

var prDetails = map[int]*github.PRDetail{
	101: {
		Number: 101, Title: "Add rate limiting middleware",
		Body:           "## Summary\nAdds per-IP rate limiting middleware using `golang.org/x/time/rate`.\n\n## Changes\n- New `RateLimiter` struct with configurable RPS and burst\n- Thread-safe visitor tracking with `sync.Mutex`\n- HTTP middleware wrapper returning 429 on limit exceeded\n\n## Testing\n- Unit tests for limiter creation and request blocking\n- Integration test with concurrent requests",
		HTMLURL:        "https://github.com/acme/gateway/pull/101",
		Author:         userAlice, Repo: repoGateway,
		BaseBranch:     "main", HeadBranch: "alice/rate-limiting",
		HeadSHA:        "a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2",
		Mergeable:      true, MergeableState: "clean",
	},
	202: {
		Number: 202, Title: "Migrate to React Server Components",
		Body:           "## Summary\nMigrates `ProductList` from client-side data fetching to React Server Components.\n\n## Motivation\n- Eliminates client-side loading spinner\n- Reduces JavaScript bundle size\n- Direct database access from server component\n\n## Breaking Changes\n- `ProductList` is now an async default export\n- Removed `useEffect`/`useState` pattern",
		HTMLURL:        "https://github.com/acme/dashboard/pull/202",
		Author:         userBob, Repo: repoDashboard,
		BaseBranch:     "main", HeadBranch: "bob/server-components",
		HeadSHA:        "b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3",
		Mergeable:      true, MergeableState: "draft",
	},
	303: {
		Number: 303, Title: "Implement async connection pool",
		Body:           "## Summary\nGeneric async connection pool using Tokio semaphore for backpressure.\n\n## Design\n- `ConnectionPool<C>` with configurable max size\n- `PoolGuard` with automatic return-to-pool on drop\n- Factory function for lazy connection creation\n\n## TODO\n- [ ] Add health checks\n- [ ] Connection timeout/eviction",
		HTMLURL:        "https://github.com/acme/nexus/pull/303",
		Author:         userCarol, Repo: repoNexus,
		BaseBranch:     "main", HeadBranch: "carol/connection-pool",
		HeadSHA:        "c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4",
		Mergeable:      true, MergeableState: "unstable",
	},
	404: {
		Number: 404, Title: "Add dependency injection for services",
		Body:           "## Summary\nRefactors `OrderService` to use constructor injection instead of direct instantiation.\n\n## Changes\n- Extract `IOrderService` interface\n- Inject `ILogger`, `IPaymentGateway`, `IInventoryService`\n- Add async operations with proper logging\n\n## Notes\nThis PR is behind main by 3 commits — will rebase once reviewed.",
		HTMLURL:        "https://github.com/acme/platform/pull/404",
		Author:         userDave, Repo: repoPlatform,
		BaseBranch:     "main", HeadBranch: "dave/dependency-injection",
		HeadSHA:        "d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5",
		Mergeable:      false, MergeableState: "behind",
		BehindBy:       3,
	},
	505: {
		Number: 505, Title: "Optimize memory allocator",
		Body:           "## Summary\nOptimize the free-list allocator with exact-fit fast path and block splitting.\n\n## Changes\n- Exact-fit allocation returns block immediately without splitting\n- Blocks above minimum split threshold are carved up\n- Added `len` tracking to `FreeList` struct\n\n## Benchmarks\n- 2.3x throughput improvement for small allocations\n- 15% reduction in fragmentation",
		HTMLURL:        "https://github.com/acme/allocator/pull/505",
		Author:         userDemo, Repo: repoAllocator,
		BaseBranch:     "main", HeadBranch: "demo-user/optimize-allocator",
		HeadSHA:        "e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6",
		Mergeable:      true, MergeableState: "clean",
	},
	606: {
		Number: 606, Title: "Add type hints to data pipeline",
		Body:           "## Summary\nAdds comprehensive type hints to the data pipeline module.\n\n## Changes\n- `PipelineConfig` dataclass for configuration\n- Type annotations on all public functions\n- `pathlib.Path` instead of raw strings for file paths\n- Improved `transform_data` with `.assign()` pattern",
		HTMLURL:        "https://github.com/acme/pipeline/pull/606",
		Author:         userDemo, Repo: repoPipeline,
		BaseBranch:     "main", HeadBranch: "demo-user/type-hints",
		HeadSHA:        "f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1",
		Mergeable:      true, MergeableState: "clean",
	},
}

// -- PR Files (diffs) --

var prFiles = map[int][]github.PRFile{
	101: {
		{
			Filename: "middleware/ratelimit.go", Status: "added",
			Additions: 45, Deletions: 0,
			Patch: `@@ -0,0 +1,45 @@
+package middleware
+
+import (
+	"net/http"
+	"sync"
+
+	"golang.org/x/time/rate"
+)
+
+// RateLimiter implements per-IP rate limiting for HTTP handlers.
+type RateLimiter struct {
+	mu       sync.Mutex
+	visitors map[string]*rate.Limiter
+	rate     rate.Limit
+	burst    int
+}
+
+// NewRateLimiter creates a rate limiter with the given requests/second and burst.
+func NewRateLimiter(rps float64, burst int) *RateLimiter {
+	return &RateLimiter{
+		visitors: make(map[string]*rate.Limiter),
+		rate:     rate.Limit(rps),
+		burst:    burst,
+	}
+}
+
+// getLimiter returns the rate limiter for the given IP, creating one if needed.
+func (rl *RateLimiter) getLimiter(ip string) *rate.Limiter {
+	rl.mu.Lock()
+	defer rl.mu.Unlock()
+
+	if limiter, exists := rl.visitors[ip]; exists {
+		return limiter
+	}
+
+	limiter := rate.NewLimiter(rl.rate, rl.burst)
+	rl.visitors[ip] = limiter
+	return limiter
+}
+
+// Middleware wraps an HTTP handler with rate limiting.
+func (rl *RateLimiter) Middleware(next http.Handler) http.Handler {
+	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
+		limiter := rl.getLimiter(r.RemoteAddr)
+		if !limiter.Allow() {
+			http.Error(w, "rate limit exceeded", http.StatusTooManyRequests)
+			return
+		}
+		next.ServeHTTP(w, r)
+	})
+}`,
		},
	},
	202: {
		{
			Filename: "components/ProductList.tsx", Status: "modified",
			Additions: 38, Deletions: 25,
			Patch: `@@ -1,25 +1,38 @@
-import React, { useEffect, useState } from 'react';
-import { fetchProducts } from '../api/products';
-import { ProductCard } from './ProductCard';
-import { Spinner } from './Spinner';
-
-export function ProductList() {
-  const [products, setProducts] = useState([]);
-  const [loading, setLoading] = useState(true);
-
-  useEffect(() => {
-    fetchProducts()
-      .then(setProducts)
-      .finally(() => setLoading(false));
-  }, []);
-
-  if (loading) return <Spinner />;
-
-  return (
-    <div className="grid grid-cols-3 gap-4">
-      {products.map((p) => (
-        <ProductCard key={p.id} product={p} />
-      ))}
-    </div>
-  );
-}
+import { db } from '@/lib/db';
+import { ProductCard } from './ProductCard';
+import { Suspense } from 'react';
+import { ErrorBoundary } from './ErrorBoundary';
+
+async function getProducts() {
+  'use server';
+  return db.product.findMany({
+    orderBy: { createdAt: 'desc' },
+    take: 50,
+  });
+}
+
+type Products = Awaited<ReturnType<typeof getProducts>>;
+
+function ProductGrid({ products }: { products: Products }) {
+  return (
+    <div className="grid grid-cols-3 gap-4">
+      {products.map((p) => (
+        <ProductCard key={p.id} product={p} />
+      ))}
+    </div>
+  );
+}
+
+export default async function ProductList() {
+  const products = await getProducts();
+
+  return (
+    <ErrorBoundary fallback={<p>Failed to load products</p>}>
+      <Suspense fallback={<div className="animate-pulse h-96" />}>
+        <ProductGrid products={products} />
+      </Suspense>
+    </ErrorBoundary>
+  );
+}`,
		},
	},
	303: {
		{
			Filename: "src/pool.rs", Status: "added",
			Additions: 52, Deletions: 0,
			Patch: `@@ -0,0 +1,52 @@
+use std::collections::VecDeque;
+use std::sync::Arc;
+use tokio::sync::{Mutex, OwnedSemaphorePermit, Semaphore};
+
+pub struct ConnectionPool<C> {
+    connections: Arc<Mutex<VecDeque<C>>>,
+    semaphore: Arc<Semaphore>,
+    factory: Box<dyn Fn() -> C + Send + Sync>,
+    max_size: usize,
+}
+
+impl<C: Send + 'static> ConnectionPool<C> {
+    pub fn new(max_size: usize, factory: impl Fn() -> C + Send + Sync + 'static) -> Self {
+        Self {
+            connections: Arc::new(Mutex::new(VecDeque::with_capacity(max_size))),
+            semaphore: Arc::new(Semaphore::new(max_size)),
+            factory: Box::new(factory),
+            max_size,
+        }
+    }
+
+    pub async fn acquire(&self) -> PoolGuard<C> {
+        let permit = self.semaphore.clone().acquire_owned().await.unwrap();
+        let conn = {
+            let mut pool = self.connections.lock().await;
+            pool.pop_front().unwrap_or_else(|| (self.factory)())
+        };
+
+        PoolGuard {
+            conn: Some(conn),
+            pool: self.connections.clone(),
+            _permit: permit,
+        }
+    }
+
+    pub async fn size(&self) -> usize {
+        self.connections.lock().await.len()
+    }
+}
+
+pub struct PoolGuard<C> {
+    conn: Option<C>,
+    pool: Arc<Mutex<VecDeque<C>>>,
+    _permit: OwnedSemaphorePermit,
+}
+
+impl<C: Send + 'static> Drop for PoolGuard<C> {
+    fn drop(&mut self) {
+        if let Some(conn) = self.conn.take() {
+            let pool = self.pool.clone();
+            tokio::spawn(async move {
+                pool.lock().await.push_back(conn);
+            });
+        }
+    }
+}`,
		},
	},
	404: {
		{
			Filename: "Services/OrderService.cs", Status: "modified",
			Additions: 32, Deletions: 18,
			Patch: `@@ -1,18 +1,35 @@
-using System;
-
 namespace Platform.Services;

-public class OrderService
+public interface IOrderService
+{
+    Task<Order> CreateOrder(CreateOrderRequest request);
+    Task<Order?> GetOrder(Guid id);
+}
+
+public class OrderService : IOrderService
 {
-    public OrderService()
+    private readonly ILogger<OrderService> _logger;
+    private readonly IPaymentGateway _paymentGateway;
+    private readonly IInventoryService _inventory;
+    private readonly IOrderRepository _repository;
+
+    public OrderService(
+        ILogger<OrderService> logger,
+        IPaymentGateway paymentGateway,
+        IInventoryService inventory,
+        IOrderRepository repository)
     {
+        _logger = logger;
+        _paymentGateway = paymentGateway;
+        _inventory = inventory;
+        _repository = repository;
     }

-    public Order CreateOrder(CreateOrderRequest request)
+    public async Task<Order> CreateOrder(CreateOrderRequest request)
     {
-        // TODO: Add validation
-        var order = new Order { Id = Guid.NewGuid(), Items = request.Items };
-        // TODO: Process payment
-        // TODO: Update inventory
+        _logger.LogInformation("Creating order for {ItemCount} items", request.Items.Count);
+        await _inventory.ReserveItems(request.Items);
+        var payment = await _paymentGateway.Charge(request.Total);
+        var order = new Order { Id = Guid.NewGuid(), Items = request.Items, PaymentId = payment.Id };
+        _logger.LogInformation("Order {OrderId} created successfully", order.Id);
         return order;
     }
+
+    public async Task<Order?> GetOrder(Guid id) => await _repository.FindAsync(id);
 }`,
		},
	},
	505: {
		{
			Filename: "src/freelist.zig", Status: "modified",
			Additions: 25, Deletions: 10,
			Patch: `@@ -12,20 +12,35 @@
 const FreeList = struct {
     head: ?*Block,
+    len: usize,

-    fn alloc(self: *FreeList, size: usize) ?*Block {
+    fn alloc(self: *FreeList, size: usize) ?[*]u8 {
         var prev: ?*Block = null;
         var current = self.head;

         while (current) |block| {
-            if (block.size >= size) {
+            if (block.size == size) {
+                // Exact fit - remove from list
                 if (prev) |p| {
                     p.next = block.next;
                 } else {
                     self.head = block.next;
                 }
-                return block;
+                self.len -= 1;
+                return @ptrCast([*]u8, block);
+            } else if (block.size >= size + @sizeOf(Block) + 16) {
+                // Split: carve out requested size, keep remainder
+                const remainder = @intToPtr(*Block, @ptrToInt(block) + size + @sizeOf(Block));
+                remainder.size = block.size - size - @sizeOf(Block);
+                remainder.next = block.next;
+                if (prev) |p| {
+                    p.next = remainder;
+                } else {
+                    self.head = remainder;
+                }
+                block.size = size;
+                return @ptrCast([*]u8, block);
             }
             prev = block;
             current = block.next;
         }
+
         return null;
     }
 };`,
		},
	},
	606: {
		{
			Filename: "pipeline/transform.py", Status: "modified",
			Additions: 35, Deletions: 22,
			Patch: `@@ -1,22 +1,35 @@
-import pandas as pd
+from __future__ import annotations

+from dataclasses import dataclass
+from pathlib import Path
+from typing import Sequence

-def load_data(path):
-    df = pd.read_csv(path)
-    return df
+import pandas as pd


-def clean_data(df):
-    df = df.dropna()
-    df = df.drop_duplicates()
+@dataclass(frozen=True)
+class PipelineConfig:
+    input_path: Path
+    output_path: Path
+    drop_columns: Sequence[str] = ()
+    fill_value: float = 0.0
+
+
+def load_data(config: PipelineConfig) -> pd.DataFrame:
+    df = pd.read_csv(config.input_path)
+    if config.drop_columns:
+        df = df.drop(columns=list(config.drop_columns), errors="ignore")
     return df


-def transform_data(df, multiplier):
-    df['value'] = df['value'] * multiplier
-    df['category'] = df['category'].str.lower()
+def clean_data(df: pd.DataFrame, *, fill_value: float = 0.0) -> pd.DataFrame:
+    df = df.fillna(fill_value)
+    df = df.drop_duplicates()
     return df


-def save_data(df, path):
-    df.to_csv(path, index=False)
+def transform_data(df: pd.DataFrame, multiplier: float) -> pd.DataFrame:
+    df = df.assign(
+        value=df["value"] * multiplier,
+        category=df["category"].str.lower().str.strip(),
+    )
+    return df
+
+
+def save_data(df: pd.DataFrame, path: Path) -> None:
+    path.parent.mkdir(parents=True, exist_ok=True)
+    df.to_csv(path, index=False)`,
		},
	},
}

// -- CI Status --

var ciStatuses = map[int]*github.CIStatus{
	101: {
		TotalCount: 3, OverallStatus: "passing",
		Checks: []github.CICheck{
			{ID: 9001, Name: "lint", Status: "completed", Conclusion: "success", HTMLURL: "https://github.com/acme/gateway/actions/runs/9001", WorkflowRunID: 9001},
			{ID: 9002, Name: "test", Status: "completed", Conclusion: "success", HTMLURL: "https://github.com/acme/gateway/actions/runs/9002", WorkflowRunID: 9001},
			{ID: 9003, Name: "build", Status: "completed", Conclusion: "success", HTMLURL: "https://github.com/acme/gateway/actions/runs/9003", WorkflowRunID: 9001},
		},
	},
	202: {
		TotalCount: 3, OverallStatus: "failing",
		Checks: []github.CICheck{
			{ID: 9011, Name: "lint", Status: "completed", Conclusion: "success", HTMLURL: "https://github.com/acme/dashboard/actions/runs/9011", WorkflowRunID: 9010},
			{ID: 9012, Name: "test", Status: "completed", Conclusion: "failure", HTMLURL: "https://github.com/acme/dashboard/actions/runs/9012", WorkflowRunID: 9010},
			{ID: 9013, Name: "typecheck", Status: "completed", Conclusion: "failure", HTMLURL: "https://github.com/acme/dashboard/actions/runs/9013", WorkflowRunID: 9010},
		},
	},
	303: {
		TotalCount: 1, OverallStatus: "pending",
		Checks: []github.CICheck{
			{ID: 9021, Name: "ci", Status: "in_progress", Conclusion: "", HTMLURL: "https://github.com/acme/nexus/actions/runs/9021", WorkflowRunID: 9020},
		},
	},
	404: {
		TotalCount: 3, OverallStatus: "mixed",
		Checks: []github.CICheck{
			{ID: 9031, Name: "build", Status: "completed", Conclusion: "success", HTMLURL: "https://github.com/acme/platform/actions/runs/9031", WorkflowRunID: 9030},
			{ID: 9032, Name: "test", Status: "completed", Conclusion: "failure", HTMLURL: "https://github.com/acme/platform/actions/runs/9032", WorkflowRunID: 9030},
			{ID: 9033, Name: "lint", Status: "completed", Conclusion: "skipped", HTMLURL: "https://github.com/acme/platform/actions/runs/9033", WorkflowRunID: 9030},
		},
	},
	505: {
		TotalCount: 2, OverallStatus: "passing",
		Checks: []github.CICheck{
			{ID: 9041, Name: "test", Status: "completed", Conclusion: "success", HTMLURL: "https://github.com/acme/allocator/actions/runs/9041", WorkflowRunID: 9040},
			{ID: 9042, Name: "build", Status: "completed", Conclusion: "success", HTMLURL: "https://github.com/acme/allocator/actions/runs/9042", WorkflowRunID: 9040},
		},
	},
	606: {
		TotalCount: 0, OverallStatus: "",
		Checks:     nil,
	},
}

// -- Reviews --

var reviewSummaries = map[int]*github.ReviewSummary{
	101: {
		Approved:       []github.Review{{Author: userBob, State: "APPROVED", Body: "Clean implementation, LGTM!", SubmittedAt: baseTime.Add(-12 * time.Hour)}},
		ReviewDecision: "APPROVED",
	},
	202: {
		ChangesRequested: []github.Review{{Author: userCarol, State: "CHANGES_REQUESTED", Body: "The 'use server' directive should be in a separate file. Also missing error handling for the db query.", SubmittedAt: baseTime.Add(-6 * time.Hour)}},
		ReviewDecision:   "CHANGES_REQUESTED",
	},
	303: {
		PendingReviewers: []github.ReviewRequest{{Login: "dave", IsTeam: false}},
		ReviewDecision:   "REVIEW_REQUIRED",
	},
	404: {
		Commented:      []github.Review{{Author: userEve, State: "COMMENTED", Body: "Good direction! A few suggestions on the DI pattern.", SubmittedAt: baseTime.Add(-24 * time.Hour)}},
		ReviewDecision: "",
	},
	505: {
		ReviewDecision: "",
	},
	606: {
		Approved: []github.Review{
			{Author: userAlice, State: "APPROVED", Body: "Type hints look great, much more readable now.", SubmittedAt: baseTime.Add(-48 * time.Hour)},
			{Author: userBob, State: "APPROVED", Body: "LGTM", SubmittedAt: baseTime.Add(-36 * time.Hour)},
		},
		ReviewDecision: "APPROVED",
	},
}

// -- Issue-level Comments --

var issueComments = map[int][]github.Comment{
	101: {
		{Author: userBob, Body: "Nice approach using `x/time/rate`. Have you considered adding a cleanup goroutine to evict stale entries from the visitors map?", CreatedAt: baseTime.Add(-20 * time.Hour)},
		{Author: userCarol, Body: "We should also add this to the middleware chain in `main.go` — want me to open a follow-up PR?", CreatedAt: baseTime.Add(-16 * time.Hour)},
	},
	606: {
		{Author: userFrank, Body: "Love the `PipelineConfig` dataclass — much cleaner than the old positional args. Should we add a `from_yaml` classmethod?", CreatedAt: baseTime.Add(-72 * time.Hour)},
	},
}

// -- Inline Comments --

var inlineComments = map[int][]github.InlineComment{
	202: {
		{
			ID: 5001, Author: userCarol,
			Body:      "`'use server'` should be at the module level or in a separate file, not inside a function. This will cause issues with the RSC bundler.",
			CreatedAt: baseTime.Add(-6 * time.Hour),
			Path:      "components/ProductList.tsx", Line: 7, Side: "RIGHT",
		},
	},
	404: {
		{
			ID: 5011, Author: userEve,
			Body:      "Consider using `IOptions<T>` pattern instead of injecting individual dependencies — it scales better as the service grows.",
			CreatedAt: baseTime.Add(-24 * time.Hour),
			Path:      "Services/OrderService.cs", Line: 16, Side: "RIGHT",
		},
		{
			ID: 5012, Author: userEve,
			Body:      "Should we wrap `ReserveItems` and `Charge` in a transaction? If payment fails after reservation, we'd have orphaned reservations.",
			CreatedAt: baseTime.Add(-24 * time.Hour),
			Path:      "Services/OrderService.cs", Line: 27, Side: "RIGHT",
		},
		{
			ID: 5013, Author: userEve,
			Body:      "This one-liner is fine for now, but `FindAsync` should handle the case where the repository throws — maybe add a try/catch or let it propagate with a meaningful exception.",
			CreatedAt: baseTime.Add(-23 * time.Hour),
			Path:      "Services/OrderService.cs", Line: 35, Side: "RIGHT",
		},
	},
}
