package engine_util

/*
An engine is a low-level system for storing key/value pairs locally (without distribution or any transaction support,
etc.). This package contains code for interacting with such engines.

引擎是用于在本地存储键/值对的低级系统（没有distribution或任何transaction支持）。该软件包包含用于与此类引擎进行交互的代码。

CF means 'column family'. A good description of column families is given in https://github.com/facebook/rocksdb/wiki/Column-Families
(specifically for RocksDB, but the general concepts are universal). In short, a column family is a key namespace.
Multiple column families are usually implemented as almost separate databases. Importantly each column family can be
configured separately. Writes can be made atomic across column families, which cannot be done for separate databases.

CF的意思是“列族”。 https://github.com/facebook/rocksdb/wiki/Column-Families中对列族进行了很好的描述
（专门针对RocksDB，但一般概念是通用的）。 简而言之，列族是关键名称空间。
通常将多个列族实现为几乎独立的数据库。 重要的是，每个列族均可单独配置。
列族与单个数据库的区别：可以使跨列族的写入成为原子的，而对于单独的数据库则无法做到。


engine_util includes the following packages:

* engines: a data structure for keeping engines required by unistore.
* write_batch: code to batch writes into a single, atomic 'transaction'.
* cf_iterator: code to iterate over a whole column family in badger.

engine_util包包括以下：

* engines：一种数据结构，用于保持unistore所需的引擎。
* write_batch：用于批量写入单个原子“事务”的代码。
* cf_iterator：在badger中遍历整个列族的代码。
*/
