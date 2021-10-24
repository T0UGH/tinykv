# 使用分布式事务和通知的大规模增量处理



## Abstract



在对文档进行网络爬取时更新web索引需要在新文档到达时不断转换现有文档的大型存储库。此任务是一类数据处理任务的一个示例，这些任务通过小型、独立的变更来转换大型数据存储库。这些任务存在于现有基础设施能力之间的差距。数据库不满足以下任务的存储或吞吐量要求：谷歌的索引系统每天在数千台机器上存储数百PB的数据并处理数十亿次更新。MapReduce和其他批处理系统无法单独处理小更新，因为它们依赖于创建大批以提高效率。



我们已经构建了Percolator，一个用于增量处理大型数据集更新的系统，并部署它来创建Google web搜索索引。通过使用Percolator**将基于批处理的索引系统替换为基于增量处理的索引系统**，我们每天处理相同数量的文档，同时将Google搜索结果中文档的平均年龄缩短50%。



## 1 Introduction



考虑构建可用于回答搜索查询的Web索引的任务。索引系统首先对web上的每一页进行爬取并对其进行处理，同时在索引上维护一组不变量。例如，**如果在多个URL下对同一内容进行爬取**，则**索引中只显示PageRank最高的URL**。每个链接也会反转，以便来自每个传出链接的锚文本附加到链接指向的页面。链接反转必须跨副本工作：如有必要，指向页面副本的链接应转发到最高PageRank副本。

> 什么是反向链接：https://zh.wikipedia.org/wiki/%E5%8F%8D%E5%90%91%E9%93%BE%E6%8E%A5



这是一个批量处理任务，可以表示为一系列MapReduce[13]操作：一个用于重复聚类，一个用于链接反转等。由于MapReduce限制了计算的并行性，所以很容易维护不变量；所有文档在开始下一个处理步骤之前完成一个处理步骤。例如，当索引系统写入指向当前最高PageRank  URL的反向链接时，我们不必担心它的PageRank同时发生变化；上一个MapReduce步骤已确定其PageRank。



现在，考虑如何在重新爬取一些小部分的Web之后更新该索引。仅在新页面上运行MapReduces是不够的，因为，例如，新页面与web其余部分之间存在链接。**MapReduces必须在整个存储库中再次运行，即在新页面和旧页面上运行**。如果有足够的计算资源，MapReduce的可伸缩性使这种方法可行，事实上，在本文描述的工作出现之前，Google的web搜索索引是以这种方式生成的。但是，重新处理整个web会丢弃在早期运行中完成的工作，并使**延迟与存储库的大小成比例，而不是与更新的大小成比例**。



索引系统可以将存储库存储在DBMS中，并在使用事务维护不变量的同时更新单个文档。然而，现有的数据库管理系统无法处理大量的数据：谷歌的索引系统在数千台机器上存储数十PB的数据。像Bigtable[9]这样的分布式存储系统可以扩展到存储库的大小，但不能提供工具来帮助程序员在并发更新时维护数据不变性。



维护web搜索索引的**理想数据处理系统**将针对**增量处理进行优化**；也就是说，**它将允许我们维护一个非常大的文档存储库，并在对每个新文档进行爬取时高效地进行更新**。考虑到系统将同时处理许多小的更新，理想的系统还将提供机制，**以便在并发更新的情况下保持不变量，并跟踪已处理的更新**。



本文的其余部分描述了一个特定的增量处理系统：Percolator。Percolator为用户提供对PB级别数据量的存储库的随机访问。随机访问允许我们单独处理文档，避免MapReduce所需的对存储库的全局扫描。**为了实现高吞吐量，许多机器上的许多线程需要同时转换存储库**，因此Percolator**提供了符合ACID的事务**，使程序员更容易推断存储库的状态；我们目前实现了**快照隔离(Snapshot isolation)语义**。



除了对并发性进行推理外，增量系统的程序员还需要跟踪增量计算的状态。为了帮助他们完成这项任务，Percolator提供了观察者：当用户指定的列发生更改时，系统将调用这些代码片段。Percolator应用程序被构造为一系列观察者；每个观察者完成一项任务，并通过写入表为“下游”观察者创建更多工作。外部进程通过将初始数据写入表来触发链中的第一个观察者。



**Percolator是专为增量处理而构建的**，并不是为了取代大多数数据处理任务的现有解决方案。MapReduce可以更好地处理结果不能分解为小更新（例如，对文件排序）的计算。此外，计算应具有很强的一致性要求；否则，Bigtable就足够了。最后，计算在某些维度上应该非常大（总数据大小、转换所需的CPU等）；不适合MapReduce或Bigtable的较小计算可以由传统DBMS处理。



在谷歌内部，Percolator的主要应用程序是准备网页以包含在live web搜索索引中。通过将索引系统转换为增量系统，我们可以在对单个文档进行爬取时对其进行处理。这将平均文档处理延迟降低了100倍，搜索结果中出现的文档的平均年龄降低了近50%（搜索结果的年龄包括索引以外的延迟，例如文档更改和爬网之间的时间）。该系统还用于将页面渲染为图像；Percolator跟踪网页和它们所依赖的资源之间的关系，因此当任何依赖的资源发生变化时，可以重新处理网页。



## 2 Design



Percolator为大规模执行增量处理提供了两个主要抽象：通过随机访问存储库的**ACID事务**和**观察者**，这是一种组织增量计算的方法。Percolator系统由在集群中的每台机器上运行的三个application组成：Percolator worker、Bigtable tablet服务器和GFS  Chunks服务器。所有观察者都链接到Percolator worker，后者扫描Bigtable中更改的列（“通知”），并在worker进程中作为函数调用调用调用相应的观察者。观察者通过向Bigtable tablet服务器发送读/写RPC来执行事务，Bigtable tablet服务器反过来向GFS服务器发送读/写RPC。该系统还依赖于两个小型服务：时间戳oracle和轻量级锁服务。时间戳oracle提供严格递增的时间戳：这是快照隔离协议正确运行所需的属性。worker使用轻量级锁服务使脏通知的搜索更加高效。



从程序员的角度来看，Percolator存储库由少量表组成。每个表都是按行和列索引的“单元格”集合。每个单元格包含一个值：一个无解释的bytes数组。（在内部，为了支持快照隔离，我们将每个单元表示为一系列按时间戳索引的值。）



Percolator的设计受到大规模运行的要求以及不需要极低延迟的宽松条件影响。例如，宽松的延迟要求允许我们采用一种懒惰的方法来清理故障机器上运行的事务留下的锁。**这种懒惰、易于实现的方法可能会将事务提交延迟数十秒**。这种延迟在运行OLTP任务的DBMS中是不可接受的，但在构建web索引的增量处理系统中是可以容忍的。Percolator没有用于事务管理的中心位置；特别是，它缺少全局死锁检测器。这会增加冲突事务的延迟，但允许系统扩展到数千台机器。



### 2.1 Bigtable overview



Percolator构建在Bigtable分布式存储系统之上。Bigtable向用户提供多维排序映射：键是（行、列、时间戳）三元组。Bigtable对每一行提供查找和更新操作，**Bigtable行事务对单个行启用原子读-修改-写操作**。Bigtable处理数PB的数据，并在大量（不可靠）机器上可靠运行。



运行中的Bigtable由一组tablet服务器组成，每个服务器负责为多个tablet（密钥空间的连续区域）提供服务。master通过引导tablet服务器加载或卸载tablet来协调tablet服务器的操作。tablet以Google SSTable格式存储为只读文件的集合。SSTables存放在GFS中；Bigtable依靠GFS在磁盘丢失时保留数据。Bigtable允许用户通过将一组列分组到本地组来控制表的性能特征。每个本地组中的列存储在各自的SSTables集合中，由于不需要扫描其他列中的数据，因此扫描它们的成本较低。



在Bigtable上构建的决策定义了Percolator的整体架构。Percolator维护了Bigtable接口的要点：数据被组织成Bigtable行和列，Percolator元数据存储在特殊列中（见图5）。Percolator的API与Bigtable的API非常相似：Percolator库主要由封装在Percolator特定计算中的Bigtable操作组成。因此，实现Percolator的挑战在于提供Bigtable所没有的特性：多行事务和观察者框架。

> 在一个只支持单行事务的分布式存储系统上，实现多行事务



### 2.2 Transactions



Percolator提供具有ACID快照隔离语义的**跨行**、**跨表事务**。Percolator用户使用命令式语言（当前为C++）编写事务代码，并将对Percolator API的调用与其代码混合。图2显示了通过散列文档内容对文档进行聚类的简化版本。



```C++
bool UpdateDocument(Document doc) {
    Transaction t(&cluster);
    t.Set(doc.url(), "contents", "document", doc.contents());
    int hash = Hash(doc.contents());
    
    // dups table maps hash ->canonical URL 标准网址
    string canonical;
    if (!t.Get(hash, "canonical-url", "dups", &canonical)) {
        // No canonical yet; write myself in 还没有标准，写入我自己的网址
        t.Set(hash, "cannonical-url", "dups", doc.url());
    } // else this doc already exists, ignore new copy 如果已经有标准，就忽略新的副本
    return t.commit();
}
```



> 图2：PercolatorAPI执行基本校验聚集并消除具有相同内容的文档的示例用法。



在本例中，如果`Commit()`返回`false`，则事务发生冲突（在本例中，因为同时处理了两个具有相同内容哈希的URL），应在退避后重试。对`Get()`和`Commit()`的调用被阻塞；并行性是通过在线程池中同时运行多个事务来实现的。



虽然可以在不使用强事务的情况下递增处理数据，但事务使用户可以更容易地对系统状态进行推理，并避免在长期存储库中引入错误。例如，在事务性web索引系统中，程序员可以做出如下假设：文档内容的哈希始终与索引duplicates的表一致。在没有事务的情况下，错误的崩溃可能会导致永久性错误：文档表中的条目与duplicates表中的任何URL都不对应。事务还可以轻松地构建总是最新且一致的索引表。请注意，这两个示例都需要跨行的事务，而不是Bigtable已经提供的单行事务。



Percolator使用Bigtable的时间戳维度存储每个数据项的多个版本。**需要多个版本来提供快照隔离**[5]，这使每个事务看起来像是在某个时间戳从一个稳定的快照读取。写入将以不同的时间戳显示。快照隔离可防止写入冲突：**如果并发运行的事务A和事务B写入同一个单元格，则最多只能提交一个**。快照隔离不提供serializability；特别是，在快照隔离下运行的事务会受到写倾斜的影响。与serializability协议相比，快照隔离的主要优点是更高效的读取。因为任何时间戳都表示一个一致的快照，所以读取单元格只需要在给定的时间戳执行Bigtable查找；不需要获取锁。图3说明了快照隔离下事务之间的关系。



![](https://files.catbox.moe/wh5h6y.png)

> 图3：快照隔离下的Transaction在开始时间戳（这里用一个开方框表示）处执行读取，在提交时间戳（闭合圆）处执行写入。在本例中，事务2不会看到来自事务1的写入，因为事务2的开始时间戳早于事务1的提交时间戳。但是，事务3将看到来自1和2的写入。Transaction 1和Transaction 2同时运行：如果它们都写入同一个单元格，则至少有一个将中止。
>
> > 也就是在快照隔离事务中，事务的运行时间段[开始时间, 提交时间)不能重叠。



由于Percolator**构建为一个访问Bigtable的客户端库**，而不是控制对存储本身的访问，因此它在实现分布式事务方面面临着与传统PDBMS不同的挑战。其他并行数据库将锁定集成到管理磁盘访问的系统组件中：由于每个节点都已调解对磁盘上数据的访问，因此它可以对请求授予锁定，并拒绝违反锁定要求的访问。



相比之下，Percolator中的任何节点都可以（而且确实）发出请求，直接修改Bigtable中的状态：并没有方便的地方来拦截流量和分配锁。因此，**Percolator必须显式地维护锁**。**在机器出现故障时，锁必须保持不变**；如果锁在提交的两个阶段之间消失，系统可能会错误地提交两个本应冲突的事务。锁服务必须提供高吞吐量；数千台机器将同时请求锁定。锁服务也应该是低延迟的；每个`Get()`操作除了需要读取数据外，还需要读取锁，我们更愿意将此延迟降至最低。考虑到这些需求，锁服务器将需要复制（以经受故障）、分布和平衡（以处理负载），并写入持久数据存储。Bigtable本身满足我们的所有要求，因此**Percolator将其锁存储在同一个Bigtable中的内存(in-memory)中的特殊列中**，该Bigtable存储数据，并在访问该行中的数据时读取或修改Bigtable行事务中的锁。



现在我们将更详细地考虑事务协议。图6显示了Percolator事务的伪代码，图4显示了事务执行期间Percolator数据和元数据的布局。图5描述了系统使用的各种元数据列。事务的构造器向时间戳oracle请求开始时间戳（第6行），该时间戳确定`Get()`看到的一致快照。对`Set()`的调用被缓冲(buffered)（第7行），直到提交时为止。提交缓冲写入的基本方法是两阶段提交，由客户端协调。不同机器上的事务通过Bigtable tablet服务器上的行事务进行交互。



![](https://files.catbox.moe/2cigta.png)

> 1. 初始状态：乔的账户包含2美元，鲍勃的账户包含10美元。



![](https://files.catbox.moe/kz5mhe.png)

> 2. 转账事务首先通过写入lock列锁定Bob的帐户余额。此锁是事务的primary lock。事务还在其开始时间戳7处写入数据。



![](https://files.catbox.moe/2mtt3m.png)

> 3. 事务现在锁定Joe的帐户并写入Joe的新余额（同样，使用的是开始时间戳）。锁是事务的secondary锁，包含对primary锁的引用（存储在“Bob”行，“bal”列）；为防止此锁因崩溃而GG，希望清理该锁的事务需要提供主锁的位置来同步清理。



![](https://files.catbox.moe/4q5mt8.png)

> 4. 事务现在已经到达提交点：它擦除主锁，并在新的时间戳（称为提交时间戳， 值为8）处用写记录替换主锁。写入记录包含指向存储数据的时间戳的指针。“Bob”行中“bal”列的未来读者现在将看到`$3`的值。写入记录包含指向存储数据的时间戳的指针。“Bob”行中“bal”列的未来读者现在将看到`$3`的值。



![](https://files.catbox.moe/7gh22c.png)

> 5. 事务通过在secondary cell中添加写记录和删除锁来完成。在这种情况下，只有一个secondary cell：乔。



> Figure 4: 此图显示了Percolator事务对两个行进行修改后执行的Bigtable写入。这笔交易把鲍勃的7美元转给了乔。每个Percolator列存储为3个Bigtable列：数据(data)、写入元数据(write)和锁元数据(lock)。Bigtable的时间戳维度显示在每个单元格中；12：“data” 表示“data”已写入Bigtable时间戳12。新写入的数据以黑体显示。



| Column     | Use                                                          | Translation                                        |
| ---------- | ------------------------------------------------------------ | -------------------------------------------------- |
| `c:lock`   | An uncommitted transaction is writing this cell; contains the location of  primary lock | 未提交的事务写入到此单元格；包含primary lock的位置 |
| `c:write`  | Committed data present; stores the Bigtable timestamp of the data | 提交的数据存在；存储数据的Bigtable时间戳           |
| `c:data`   | Stores the data itself                                       | 存储数据本身                                       |
| `c:notify` | Hint: observers may need to run                              |                                                    |
| `c:ack_O`  | Observer “O” has run ; stores start timestamp<br/>of successful last run |                                                    |

> Figure 5:  名为"c"的Percolator列的Bigtable表示中的列



```c++
class Transaction {
	struct Write {Row row; Column col; string value;};
	vector<Write> writes;
	int start_ts_;
    
	Transaction(): start_ts_(oracle.GetTimestamp()){}
	
    void Set(Write w) { write_push_back(w);}
	
    bool Get(Row row, Column col, string* value) {
		while(true) {
			bigtable::Txn T = bigtable::StartRowTransaction(row);
			// check for locks that signal concurrent writes.
			if (T.Read(row, col + "lock", [0, start_ts_])) {
				// There is a pending lock: try to clean it and wait
				BackoffAndMaybeCleanupLock(row, col);
				continue;
			}
			// Find the latest write below our start_timestamp
			latest_write = T.Read(row, col + "write", [0, start_ts_]);
			if(!latest_write.found()) return false; // no data
			int data_ts = latest_write.start_timestamp();
			*value = T.Read(row, col + "data", [data_ts, data_ts]);
			return true;
		}
	}
    
    // Prewrite tries to lock cell w, returing false in case of conflict
    bool Prewrite(Write w, Write primary){
        Column c = w.col;
        bigtable::Txn T = bigtable::StartRowTransaction(w, row);
        // Abort on writes after our start timestamp ...
        if(T.Read(w.row, c+"write", [start_ts_,MAX_TS]))
            return false;
        if(T.Read(w.row, c+"lock", [0,MAX_TS]))
            return false;
        T.Write(w.row, c+"data", start_ts_, w.value);
		T.Write(w.row, c+"lock", start_ts_, {primary.row,primary.col});
        return T.Commit();
    }
    
    bool Commit() {
        Write primary = writes_[0];
        vector<Write> secondaries(writes_.begin() + 1, writes_.end());
        if (!Prewrite(primary, primary))
            return false;
        for (Write w : secondaries){
            if (!Prewrite(w, primary))
                return false;
        }
        int commit_ts = oracle.GetTimestamp();
        
        // Commit Primary first
        Write p = primary;
        bigTable::Txn T = bigtable::StartRowTransaction(p.row);
        if (!T.Read(p.row, p.col + "lock", [start_ts_, start_ts_)]))
            return false;
        T.Write(p.row, p.col + "write", commit_ts, start_ts_);
        T.Erase(p.row, p.col + "lock", commit_ts);
        if (!T.Commit())
            return false;
        
        for (Write w: secondaries) {
            bigtable::Write(w.row, w.col + "write", commit_ts, start_ts_);
            bigtable::Erase(w.row, w.col + "lock", commit_ts);
        }
        return true;
    }
}
```

> Figure 6:Pseudocode for Percolator transaction protocol. Percolator事务协议的伪代码



在提交的第一阶段（“预写”），我们尝试锁定所有正在写入的单元。（为了处理客户端故障，我们任意指定一个锁作为主锁；我们将在下面讨论这种机制。) 事务读取元数据以检查正在写入的每个单元格中是否存在冲突。有两种相互冲突的元数据：如果事务在其开始时间戳之后看到另一个写入记录，则它将中止（第33行）；这是快照隔离所防止的写-写冲突。如果事务在任何时间戳看到另一个锁，它也会中止（第35行）。这可能是另一个事务还没来得及释放它的锁但是已经在我们的开始时间戳之前提交了，但我们假设这种情况不存在，所以我们中止。如果没有冲突，我们在开始时间戳（第37-38行）将锁和数据写入每个单元格。



> 以上 Prewrite 流程任何一步发生错误，都会进行回滚：删除 Lock，删除版本为 startTs 的数据。



如果没有单元格冲突，事务可以提交并进入第二阶段。在第二阶段开始时，客户端从时间戳oracle（第51行）获取提交时间戳。然后，在每个单元格（从Primary开始），客户端释放其锁，并通过用写记录替换锁，使其写操作对读者可见。写入记录向reader指示此单元格中存在提交的数据；它包含一个指向开始时间戳的指针，读者可以用这个指针找到实际的数据。一旦primary key 的写操作可见（第58行），事务就必须提交，因为它使reader可以看到写操作。



> 如果 primary row 提交失败的话，全事务回滚，回滚逻辑同 prewrite。如果 commit primary 成功，则可以异步的 commit secondaries, 流程和 commit primary 一致， 失败了也无所谓。



`Get()`操作首先检查时间戳范围`[0，start_timestamp]`中的锁，该范围是事务快照中可见的时间戳范围（第12行）。如果存在锁，另一个事务正在同时写入此单元格，因此读事务必须等待锁释放。如果未找到冲突锁，`Get()`将读取该时间戳范围内的最新写入记录（第20行），并返回与该写入记录对应的数据项（第23行）。



客户端故障的可能性使事务处理变得复杂（tablet服务器故障不会影响系统，因为Bigtable保证在tablet服务器故障期间保持写锁）。如果在提交事务时客户端失败，则会留下锁。Percolator必须清理这些锁，否则它们将导致未来的事务无限期挂起。Percolator采用一种惰性的清理方法：当事务A遇到事务B留下的冲突锁时，A可能会确定B失败并擦除其锁。



A很难完全相信B是失败的；因此，我们必须避免A清理B的事务和提交同一事务的没有实际失败B之间的竞争。Percolator通过在每个事务中指定一个单元格作为任何提交或清理操作的同步点来处理此问题。此单元格的锁称为主锁。A和B都同意哪个锁是主锁（主锁的位置写入所有其他单元格的锁中）。执行清理或提交操作需要修改主锁；由于此修改是在Bigtable行事务下执行的，因此清理或提交操作中只有一个会成功。特别是：在B提交之前，它必须检查它是否仍然持有主锁，并用写记录替换它。在A擦除B的锁之前，A必须检查主锁以确保B没有提交；如果主锁仍然存在，则可以安全地擦除该锁。



当客户端在提交的第二阶段崩溃时，事务将超过提交点（它至少写入了一条写入记录），但仍有未完成的锁。我们必须对这些事务执行前滚。遇到锁的事务可以通过检查主锁来区分这两种情况：如果主锁已被写入记录替换，则写入该锁的事务必须已提交，并且必须前滚锁，否则应将其回滚（因为我们总是先提交primary，所以如果primary未提交，我们可以确保回滚是安全的）。要向前滚动，执行清理的事务将使用写入记录替换GG的锁，就像原始事务的处理逻辑一样。



因为清理是在主锁上同步的，所以清理活动客户端持有的锁是安全的；但是，这会导致性能损失，因为回滚会强制事务中止。因此，事务不会清理锁，除非它怀疑锁属于已死亡或卡住的worker。Percolator使用简单的机制来确定另一个事务的活跃度。正在运行的worker在Chubby  lockservice[8]中写入一个令牌，表示他们属于该系统；其他worker可以使用此令牌的存在作为该worker处于活动状态的标志（进程退出时，令牌会自动清除）。为了处理一个上线了但没有工作的工人，我们在锁中额外写入了超时时间(wall time)；即使worker的活跃度令牌有效，时间超时的锁也将被清除。为了处理长时间运行的提交操作，worker在提交时会定期更新此超时时间。



### 2.3 Timestamps



时间戳oracle是一个按严格递增顺序分发时间戳的服务器。由于每个事务都需要联系oracle两次时间戳，因此此服务必须具有良好的扩展性。oracle通过将一个周期的最高分配的时间戳写入稳定存储器，周期性地分配一系列时间戳；给定一个分配的时间戳范围（周期），oracle可以严格满足来自内存的未来请求。如果oracle重新启动，时间戳将向前跳到分配的最大时间戳（但不会向后跳）。为了节省RPC开销（以增加事务延迟为代价），每个Percolator worker通过只维护一个到oracle的 pending RPC 来跨事务批量地进行时间戳请求。随着oracle负载的增加，批的大小自然会增加以进行补偿。批处理增加了oracle的可伸缩性，但不影响时间戳保证。我们的oracle每秒从一台机器上提供大约200万个时间戳。



事务协议使用严格递增的时间戳来保证`Get()`返回所有在事务的开始时间戳之前提交的写操作。要了解它是如何提供这种保证的，考虑一个事务R在时间戳$T_R$读， 而一个事务W在时间戳$T_W$提交，且$T_W < T_R$；我们将显示R看到W的写入。由于$T_W < T_R$，我们知道时间戳oracle在$T_R$之前或在同一批中给出了$T_W$; 因此，W在R接收到$T_R$之前请求了$T_W$。我们知道R在接收到它的开始时间戳$T_R$之前不能执行读操作，而W在请求它的提交时间戳$T_W$之前已经写入了锁记录。因此，上述属性保证了W一定在R进行任何读操作之前写完它的所有锁;R的`Get()`将看到完全提交的写记录或锁，如果R看到了锁，将阻塞直到锁被释放。不管怎样，W的write对R的Get()是可见的。

> $T_W < T_R$ 确保了W的lock一定在R的read之前



### 2.4 Notifications



略



### 2.5 Discussion



略



# 补充： https://pingcap.com/zh/blog/percolator-and-txn



本文先概括的讲一下 Google Percolator 的大致流程。Percolator 是 Google 的上一代分布式事务解决方案，构建在 BigTable 之上，在 Google 内部 用于网页索引更新的业务，原始的论文 [在此 ](http://research.google.com/pubs/pub36726.html)。原理比较简单，总体来说就是一个经过优化的二阶段提交的实现，进行了一个二级锁的优化。TiDB 的事务模型沿用了 Percolator 的事务模型。 总体的流程如下：

### 读写事务

1. 事务提交前，在客户端 buffer 所有的 update/delete 操作。
2. Prewrite 阶段:

首先在所有行的写操作中选出一个作为 primary，其他的为 secondaries。

PrewritePrimary: 对 primaryRow 写入 L 列(上锁)，L 列中记录本次事务的开始时间戳。写入 L 列前会检查:

1. 是否已经有别的客户端已经上锁 (Locking)。
2. 是否在本次事务开始时间之后，检查 W 列，是否有更新 [startTs, +Inf) 的写操作已经提交 (Conflict)。

在这两种情况下会返回事务冲突。否则，就成功上锁。将行的内容写入 row 中，时间戳设置为 startTs。

将 primaryRow 的锁上好了以后，进行 secondaries 的 prewrite 流程:

1. 类似 primaryRow 的上锁流程，只不过锁的内容为事务开始时间及 primaryRow 的 Lock 的信息。
2. 检查的事项同 primaryRow 的一致。

当锁成功写入后，写入 row，时间戳设置为 startTs。

1. 以上 Prewrite 流程任何一步发生错误，都会进行回滚：删除 Lock，删除版本为 startTs 的数据。
2. 当 Prewrite 完成以后，进入 Commit 阶段，当前时间戳为 commitTs，且 commitTs> startTs :

1. commit primary：写入 W 列新数据，时间戳为 commitTs，内容为 startTs，表明数据的最新版本是 startTs 对应的数据。
2. 删除L列。

如果 primary row 提交失败的话，全事务回滚，回滚逻辑同 prewrite。如果 commit primary 成功，则可以异步的 commit secondaries, 流程和 commit primary 一致， 失败了也无所谓。

### 事务中的读操作

1. 检查该行是否有 L 列，时间戳为 [0, startTs]，如果有，表示目前有其他事务正占用此行，如果这个锁已经超时则尝试清除，否则等待超时或者其他事务主动解锁。注意此时不能直接返回老版本的数据，否则会发生幻读的问题。
2. 读取至 startTs 时该行最新的数据，方法是：读取 W 列，时间戳为 [0, startTs], 获取这一列的值，转化成时间戳 t, 然后读取此列于 t 版本的数据内容。

由于锁是分两级的，primary 和 seconary，只要 primary 的行锁去掉，就表示该事务已经成功 提交，这样的好处是 secondary 的 commit 是可以异步进行的，只是在异步提交进行的过程中 ，如果此时有读请求，可能会需要做一下锁的清理工作。



