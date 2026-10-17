import 'package:flutter/material.dart';

import 'core_bridge.dart';
import 'local_store.dart';
import 'models.dart';

/// 发布页域名采集：为站源配置发布页地址与域名提取规则，
/// 站源内置域名失效时自动从发布页采集最新可用域名。
class PublishSourcesScreen extends StatefulWidget {
  const PublishSourcesScreen({
    super.key,
    required this.repository,
    required this.store,
  });
  final AppRepository repository;
  final LocalStore store;

  @override
  State<PublishSourcesScreen> createState() => _PublishSourcesScreenState();
}

class _PublishSourcesScreenState extends State<PublishSourcesScreen> {
  List<PublishSourceConfig>? _items;
  String? _error;
  bool _busy = false;

  @override
  void initState() {
    super.initState();
    _reload();
  }

  Future<void> _reload() async {
    try {
      final items = await widget.repository.publishSources();
      if (!mounted) return;
      setState(() {
        _items = items;
        _error = null;
      });
    } catch (error) {
      if (!mounted) return;
      setState(() => _error = error.toString());
    }
  }

  void _notice(String message, {bool error = false}) {
    if (!mounted) return;
    ScaffoldMessenger.of(context).showSnackBar(
      SnackBar(
        content: Text(message),
        backgroundColor: error ? Theme.of(context).colorScheme.error : null,
      ),
    );
  }

  Future<void> _run(Future<void> Function() action) async {
    if (_busy) return;
    setState(() => _busy = true);
    try {
      await action();
    } catch (error) {
      _notice(error.toString(), error: true);
    } finally {
      if (mounted) setState(() => _busy = false);
    }
  }

  String _sourceName(String id) {
    for (final site in widget.store.sources) {
      if (site.id == id) return site.name;
    }
    return SourceSite.byId(id).id == id ? SourceSite.byId(id).name : id;
  }

  Future<void> _edit([PublishSourceConfig? existing]) => showDialog<void>(
    context: context,
    builder: (_) => _PublishSourceDialog(
      store: widget.store,
      repository: widget.repository,
      existing: existing,
      takenSources: (_items ?? const [])
          .map((item) => item.source)
          .where((id) => existing == null || id != existing.source)
          .toSet(),
    ),
  ).then((saved) {
    if (saved == true) _reload();
  });

  Future<void> _refresh(PublishSourceConfig item) => _run(() async {
    final base = await widget.repository.refreshPublishSource(item.source);
    _notice('已采集到可用域名：$base');
    await _reload();
  });

  Future<void> _remove(PublishSourceConfig item) => _run(() async {
    await widget.repository.removePublishSource(item.source);
    _notice('已删除 ${_sourceName(item.source)} 的发布页配置');
    await _reload();
  });

  @override
  Widget build(BuildContext context) {
    final items = _items;
    return Scaffold(
      appBar: AppBar(title: const Text('发布页域名采集')),
      floatingActionButton: FloatingActionButton.extended(
        heroTag: 'publish-add',
        onPressed: _busy ? null : () => _edit(),
        icon: const Icon(Icons.add_rounded),
        label: const Text('添加发布页'),
      ),
      body: items == null
          ? Center(
              child: _error != null
                  ? Column(
                      mainAxisSize: MainAxisSize.min,
                      children: [
                        Text(_error!, textAlign: TextAlign.center),
                        const SizedBox(height: 12),
                        FilledButton.icon(
                          onPressed: _reload,
                          icon: const Icon(Icons.refresh_rounded),
                          label: const Text('重试'),
                        ),
                      ],
                    )
                  : const CircularProgressIndicator.adaptive(),
            )
          : ListView(
              padding: const EdgeInsets.all(16) + const EdgeInsets.only(bottom: 88),
              children: [
                Text(
                  '站源内置域名失效时，会自动从发布页抓取最新域名并探测可用性，无需重新安装。'
                  '采集结果持久保存，后台每 20 分钟自动复查一次。',
                  style: Theme.of(context).textTheme.bodySmall?.copyWith(
                    color: Theme.of(context).colorScheme.onSurfaceVariant,
                  ),
                ),
                const SizedBox(height: 16),
                if (items.isEmpty)
                  Card(
                    child: Padding(
                      padding: const EdgeInsets.all(20),
                      child: Column(
                        children: [
                          Icon(
                            Icons.public_rounded,
                            size: 42,
                            color: Theme.of(context).colorScheme.primary,
                          ),
                          const SizedBox(height: 10),
                          const Text('还没有配置发布页'),
                          const SizedBox(height: 4),
                          Text(
                            '黄果、帝果等境外站源域名经常更换，配置发布页后可自动恢复。',
                            textAlign: TextAlign.center,
                            style: Theme.of(context).textTheme.bodySmall,
                          ),
                        ],
                      ),
                    ),
                  ),
                for (final (index, item) in items.indexed) ...[
                  if (index > 0) const SizedBox(height: 12),
                  Card(
                    child: ListTile(
                      title: Text(
                        _sourceName(item.source),
                        style: const TextStyle(fontWeight: FontWeight.w700),
                      ),
                      subtitle: Padding(
                        padding: const EdgeInsets.only(top: 4),
                        child: Column(
                          crossAxisAlignment: CrossAxisAlignment.start,
                          children: [
                            Text('发布页：${item.url}'),
                            if (item.rule.isNotEmpty) Text('规则：${item.rule}'),
                            const SizedBox(height: 4),
                            item.current.isEmpty
                                ? Text(
                                    '尚未采集到可用域名',
                                    style: TextStyle(
                                      color: Theme.of(
                                        context,
                                      ).colorScheme.error,
                                    ),
                                  )
                                : Text(
                                    '当前域名：${item.current}',
                                    style: TextStyle(
                                      color: Theme.of(
                                        context,
                                      ).colorScheme.primary,
                                      fontWeight: FontWeight.w600,
                                    ),
                                  ),
                          ],
                        ),
                      ),
                      isThreeLine: true,
                      trailing: _busy
                          ? const SizedBox(
                              width: 22,
                              height: 22,
                              child: CircularProgressIndicator.adaptive(
                                strokeWidth: 2,
                              ),
                            )
                          : PopupMenuButton<String>(
                              onSelected: (action) {
                                if (action == 'refresh') _refresh(item);
                                if (action == 'edit') _edit(item);
                                if (action == 'remove') _remove(item);
                              },
                              itemBuilder: (_) => const [
                                PopupMenuItem(
                                  value: 'refresh',
                                  child: Text('立即采集'),
                                ),
                                PopupMenuItem(
                                  value: 'edit',
                                  child: Text('编辑'),
                                ),
                                PopupMenuItem(
                                  value: 'remove',
                                  child: Text('删除'),
                                ),
                              ],
                            ),
                    ),
                  ),
                ],
              ],
            ),
    );
  }
}

class _PublishSourceDialog extends StatefulWidget {
  const _PublishSourceDialog({
    required this.store,
    required this.repository,
    required this.existing,
    required this.takenSources,
  });
  final LocalStore store;
  final AppRepository repository;
  final PublishSourceConfig? existing;
  final Set<String> takenSources;

  @override
  State<_PublishSourceDialog> createState() => _PublishSourceDialogState();
}

class _PublishSourceDialogState extends State<_PublishSourceDialog> {
  final _url = TextEditingController();
  final _rule = TextEditingController();
  SourceSite? _source;
  bool _busy = false;
  String? _error;

  @override
  void initState() {
    super.initState();
    final existing = widget.existing;
    if (existing != null) {
      // 自定义源不在 SourceSite.byId 的内置表里，按可见站源列表回填。
      for (final site in widget.store.sources) {
        if (site.id == existing.source) {
          _source = site;
          break;
        }
      }
      _url.text = existing.url;
      _rule.text = existing.rule;
    }
  }

  @override
  void dispose() {
    _url.dispose();
    _rule.dispose();
    super.dispose();
  }

  List<SourceSite> get _selectable => [
    for (final site in widget.store.sources)
      if (!widget.takenSources.contains(site.id)) site,
  ];

  Future<void> _save() async {
    final source = _source;
    final url = _url.text.trim();
    if (source == null) {
      setState(() => _error = '请选择站源');
      return;
    }
    if (url.isEmpty || !url.startsWith('http')) {
      setState(() => _error = '发布页地址需为 http 或 https 开头的完整地址');
      return;
    }
    setState(() {
      _busy = true;
      _error = null;
    });
    try {
      final result = await widget.repository.savePublishSource(
        source.id,
        url: url,
        rule: _rule.text.trim(),
      );
      if (!mounted) return;
      final refreshed = result['refreshed'] == true;
      final base = result['base'] as String? ?? '';
      final warning = result['warning'] as String? ?? '';
      Navigator.pop(context, true);
      ScaffoldMessenger.of(context).showSnackBar(
        SnackBar(
          content: Text(
            refreshed
                ? '${source.name} 已采集到可用域名：$base'
                : '${source.name} 配置已保存，但本次采集失败：$warning 后台会自动重试。',
          ),
        ),
      );
    } catch (error) {
      setState(() {
        _busy = false;
        _error = error.toString();
      });
    }
  }

  @override
  Widget build(BuildContext context) {
    final selectable = _selectable;
    return AlertDialog(
      title: Text(widget.existing == null ? '添加发布页' : '编辑发布页'),
      content: SingleChildScrollView(
        child: Column(
          mainAxisSize: MainAxisSize.min,
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            DropdownButtonFormField<SourceSite>(
              key: const ValueKey('publish-source-select'),
              value: selectable.any((site) => site.id == _source?.id)
                  ? _source
                  : null,
              decoration: const InputDecoration(labelText: '站源'),
              items: [
                for (final site in selectable)
                  DropdownMenuItem(value: site, child: Text(site.name)),
              ],
              onChanged: (value) => setState(() => _source = value),
            ),
            const SizedBox(height: 12),
            TextField(
              key: const ValueKey('publish-url-input'),
              controller: _url,
              keyboardType: TextInputType.url,
              decoration: const InputDecoration(
                labelText: '发布页地址',
                hintText: 'https://example.com/publish',
              ),
            ),
            const SizedBox(height: 12),
            TextField(
              key: const ValueKey('publish-rule-input'),
              controller: _rule,
              decoration: const InputDecoration(
                labelText: '域名规则（可选）',
                hintText: '.example.com 或正则表达式',
              ),
            ),
            const SizedBox(height: 10),
            Text(
              '规则说明：留空则提取发布页里的全部链接域名；填写「.example.com」只保留该后缀的域名；'
              '也可以填正则表达式，取捕获组内容作为域名。',
              style: Theme.of(context).textTheme.bodySmall?.copyWith(
                color: Theme.of(context).colorScheme.onSurfaceVariant,
              ),
            ),
            if (_error != null) ...[
              const SizedBox(height: 10),
              Text(
                _error!,
                style: TextStyle(color: Theme.of(context).colorScheme.error),
              ),
            ],
          ],
        ),
      ),
      actions: [
        TextButton(
          onPressed: _busy ? null : () => Navigator.pop(context),
          child: const Text('取消'),
        ),
        FilledButton(
          key: const ValueKey('publish-save'),
          onPressed: _busy ? null : _save,
          child: _busy
              ? const SizedBox(
                  width: 18,
                  height: 18,
                  child: CircularProgressIndicator.adaptive(strokeWidth: 2),
                )
              : const Text('保存并采集'),
        ),
      ],
    );
  }
}
