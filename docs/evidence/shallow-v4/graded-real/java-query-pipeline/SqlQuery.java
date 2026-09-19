package io.riverdb.sql;

import io.riverdb.base.error.StatusCode;
import io.riverdb.base.sql.SqlShapeLimits;

/** Caller-owned bounded query-block graph used while compiling nested SELECTs. */
public final class SqlQuery {
  public static final int MAXIMUM_QUERY_BLOCKS = SqlShapeLimits.MAX_QUERY_BLOCKS;
  public static final int MAXIMUM_EDGES = MAXIMUM_QUERY_BLOCKS - 1;

  public static final int SUBQUERY_SCALAR = 1;
  public static final int SUBQUERY_EXISTS = 2;
  public static final int SUBQUERY_MEMBERSHIP = 3;
  public static final int SET_LEAF = 1;
  public static final int SET_UNION_ALL = 2;
  public static final int SET_UNION_DISTINCT = 3;

  private final SqlCommand[] blocks = new SqlCommand[MAXIMUM_QUERY_BLOCKS];
  private final SqlSubqueryGraph graph = new SqlSubqueryGraph();
  private final SqlSetExpression setExpression = new SqlSetExpression();
  private final SqlDerivedQueryCompiler derivedCompiler = new SqlDerivedQueryCompiler(this);
  private final SqlViewCompiler viewCompiler = new SqlViewCompiler(this, derivedCompiler);
  private int blockCount;
  private int sourceBlockCount;
  private int sourcePlanDepth;
  private boolean explain;
  private boolean analyze;
  private boolean blockPipeline;

  public SqlQuery() {
    for (int index = 0; index < blocks.length; index++) {
      blocks[index] = new SqlCommand();
    }
  }

  public void reset() {
    for (int index = 0; index < blockCount; index++) {
      blocks[index].reset();
    }
    blockCount = 0;
    graph.reset();
    setExpression.reset();
    sourceBlockCount = 0;
    sourcePlanDepth = 0;
    explain = false;
    analyze = false;
    blockPipeline = false;
  }

  void setExplain(boolean analyzeQuery) {
    explain = true;
    analyze = analyzeQuery;
  }

  public boolean isExplain() {
    return explain;
  }

  public boolean isAnalyze() {
    return analyze;
  }

  SqlCommand nextBlock() {
    if (blockCount >= blocks.length) {
      return null;
    }
    SqlCommand block = blocks[blockCount++];
    if (graph.root() < 0 || blockCount - 1 <= graph.root()) {
      sourceBlockCount = Math.max(sourceBlockCount, blockCount);
      sourcePlanDepth = Math.max(sourcePlanDepth, sourceBlockCount);
    }
    block.reset();
    return block;
  }

  void beginNestedGraph(int rootBlock) {
    graph.begin(rootBlock);
  }

  int addSubqueryEdge(int parent, int kind) {
    return graph.append(parent, kind, blockCount);
  }

  void setSubqueryEdgeLeaf(int edge, int leaf) {
    graph.setLeaf(edge, leaf);
  }

  void setSubqueryEdgeChild(int edge, int child) {
    graph.setChild(edge, child, blockCount);
  }

  int appendSetLeaf(int block) { return setExpression.appendLeaf(block); }
  int appendSetUnion(int kind, int left, int right) {
    return setExpression.appendUnion(kind, left, right);
  }
  StatusCode finishSetLeaf(int node) {
    StatusCode status = graph.count() == 0 ? StatusCode.OK : validateNestedGraph();
    if (status.isOk() && graph.count() > 0) {
      status = validNestedChildren(setExpression.block(node) + 1);
    }
    if (status.isOk()) status = setExpression.captureLeaf(node, graph);
    graph.reset();
    return status;
  }
  SqlIdentifier appendSetOrder() { return setExpression.appendOrder(); }
  void setSetOrderDescending(int expression, boolean descending) {
    setExpression.orderDescending(expression, descending);
  }
  void setSetRowLimit(long limit) { setExpression.rowLimit(limit); }
  SqlCommand firstSetBlock() {
    int node = setExpression.root();
    for (int depth = 0; depth < setExpression.count(); depth++) {
      if (setExpression.kind(node) == SET_LEAF) return block(setExpression.block(node));
      node = setExpression.left(node);
    }
    return null;
  }
  StatusCode publishSetResult(SqlCommand result) {
    SqlCommand first = firstSetBlock();
    return first == null ? StatusCode.INVALID_EXTERNAL_INPUT : setExpression.publish(first, result);
  }


  StatusCode compileDerived(SqlCommand destination) {
    if (destination == null) {
      return StatusCode.INVALID_EXTERNAL_INPUT;
    }
    destination.reset();
    if (graph.count() > 0) {
      StatusCode graphStatus = validateNestedGraph();
      if (!graphStatus.isOk()) return graphStatus;
      graphStatus = validNestedChildren(sourceBlockCount);
      if (!graphStatus.isOk()) return graphStatus;
    }
    if (SqlQueryPipelineRouting.requiresCardinalityPipeline(
        graph, blocks, sourceBlockCount)) {
      StatusCode status = derivedCompiler.compilePipeline(destination);
      blockPipeline = status.isOk();
      return status;
    }
    return derivedCompiler.compile(destination);
  }

  public boolean isBlockPipeline() { return blockPipeline; }

  void markBlockPipeline() { blockPipeline = true; }

  public StatusCode promoteRootBlockPipeline(SqlCommand command) {
    if (command == null) return StatusCode.INVALID_EXTERNAL_INPUT;
    if (blockCount != 0) {
      // The compiled command remains the authoritative root when the engine
      // captures this existing view or predicate-subquery graph.  Promotion
      // only changes its physical route; appending another root would
      // duplicate the graph's root block.
      if (sourceBlockCount < 1) return StatusCode.INVALID_EXTERNAL_INPUT;
      blockPipeline = true;
      return StatusCode.OK;
    }
    StatusCode status = appendRootBlock(command);
    if (status.isOk()) blockPipeline = true;
    return status;
  }

  public StatusCode appendRootBlock(SqlCommand command) {
    if (command == null || blockCount != 0) return StatusCode.INVALID_EXTERNAL_INPUT;
    SqlCommand block = nextBlock();
    return block == null ? StatusCode.QUERY_TOO_COMPLEX : block.copyBlockFrom(command);
  }

  public StatusCode compileBlockPipeline(SqlCommand destination) {
    if (destination == null || sourceBlockCount < 2) {
      return StatusCode.INVALID_EXTERNAL_INPUT;
    }
    StatusCode status = derivedCompiler.compilePipeline(destination);
    blockPipeline = status.isOk();
    return status;
  }

  public StatusCode compileCombined(SqlCommand destination) {
    return compileDerived(destination);
  }

  public StatusCode validateAppendedPipeline(int firstBlock) {
    return derivedCompiler.validatePipeline(firstBlock);
  }

  void discardJoinChains() {
    for (int index = 0; index < blockCount; index++) {
      blocks[index].discardJoinChain();
    }
  }

  public StatusCode expandRootSelectAllFrom(int sourceBlock) {
    if (sourceBlockCount < 2 || sourceBlock <= 0 || sourceBlock >= sourceBlockCount) {
      return StatusCode.INVALID_EXTERNAL_INPUT;
    }
    return blocks[0].isSelectAll()
        ? blocks[0].expandSelectAllFrom(blocks[sourceBlock]) : StatusCode.OK;
  }

  public StatusCode compileView(
      SqlCommand outer,
      SqlCommand view,
      SqlCommand destination) {
    return viewCompiler.compile(
        outer,
        view,
        destination,
        Math.max(1, sourcePlanDepth),
        1,
        explain,
        analyze);
  }

  public StatusCode compileExpandedView(
      SqlCommand outer,
      SqlCommand view,
      SqlCommand destination,
      int outerSourceDepth,
      int viewSourceDepth,
      boolean retainedExplain,
      boolean retainedAnalyze) {
    return viewCompiler.compile(
        outer,
        view,
        destination,
        outerSourceDepth,
        viewSourceDepth,
        retainedExplain,
        retainedAnalyze);
  }

  void setSourceMetadata(int depth, boolean explained, boolean analyzed) {
    sourcePlanDepth = depth;
    explain = explained;
    analyze = analyzed;
  }

  StatusCode compileNestedGraph(SqlCommand destination) {
    if (destination == null || blockCount < 2 || graph.count() == 0) {
      return StatusCode.INVALID_EXTERNAL_INPUT;
    }
    StatusCode graphStatus = validateNestedGraph();
    if (!graphStatus.isOk()) return graphStatus;
    StatusCode children = validNestedChildren(sourceBlockCount);
    if (!children.isOk()) return children;
    destination.reset();
    blockPipeline = SqlQueryPipelineRouting.requiresNestedPipeline(graph, blocks, blockCount);
    return destination.copyBlockFrom(blocks[0]);
  }

  private StatusCode validNestedChildren(int firstBlock) {
    for (int block = firstBlock; block < blockCount; block++) {
      SqlCommand command = blocks[block];
      if (command.type() != SqlCommandType.SCAN
          && command.type() != SqlCommandType.SELECT
          && command.type() != SqlCommandType.JOIN_SCAN
          || command.isSelectAll() || command.columnCount() != 1
          || (command.orderBy().count() > 0)) {
        return StatusCode.FEATURE_NOT_SUPPORTED;
      }
    }
    return StatusCode.OK;
  }

  StatusCode validateNestedGraph() {
    return graph.validate(this);
  }

  public int blockCount() {
    return blockCount;
  }

  boolean hasSelectForUpdate() {
    for (int index = 0; index < blockCount; index++) {
      if (blocks[index].isSelectForUpdate()) return true;
    }
    return false;
  }

  boolean childHasSelectForUpdate() {
    for (int index = 1; index < blockCount; index++) {
      if (blocks[index].isSelectForUpdate()) return true;
    }
    return false;
  }

  public int sourceBlockCount() { return sourceBlockCount; }

  public int edgeCount() { return graph.count(); }

  public int edgeKind(int edge) {
    return graph.kind(edge);
  }

  public int edgeParent(int edge) {
    return graph.parent(edge);
  }

  public int edgeLeaf(int edge) {
    return graph.leaf(edge);
  }

  public int edgeChild(int edge) {
    return graph.child(edge);
  }

  public int blockParent(int block) {
    return graph.blockParent(block);
  }

  public int blockDepth(int block) {
    return graph.blockDepth(block);
  }

  public int nestedPlanDepth() { return graph.maximumDepth(); }

  public int sourcePlanDepth() {
    return Math.max(1, sourcePlanDepth);
  }

  public boolean hasNestedTopology() {
    return graph.count() > 0;
  }

  public boolean hasSetExpression() { return setExpression.count() > 0; }
  public int setNodeCount() { return setExpression.count(); }
  public int setRootNode() { return setExpression.root(); }
  public int setNodeKind(int node) { return setExpression.kind(node); }
  public int setLeftNode(int node) { return setExpression.left(node); }
  public int setRightNode(int node) { return setExpression.right(node); }
  public int setLeafBlock(int node) { return setExpression.block(node); }
  public int setOrderExpressionCount() { return setExpression.orderCount(); }
  public SqlIdentifier setOrderColumnName(int expression) {
    return setExpression.orderName(expression);
  }
  public boolean isSetOrderDescending(int expression) {
    return setExpression.orderDescending(expression);
  }
  public long setRowLimit() { return setExpression.rowLimit(); }

  public StatusCode copySetLeafQuery(
      int rootBlock, SqlQuery destination, SqlCommand result) {
    if (destination == null || destination == this || result == null) {
      return StatusCode.INVALID_EXTERNAL_INPUT;
    }
    return setExpression.copyLeaf(rootBlock, this, destination, result);
  }

  public SqlCommand block(int index) {
    return index >= 0 && index < blockCount ? blocks[index] : null;
  }

}
