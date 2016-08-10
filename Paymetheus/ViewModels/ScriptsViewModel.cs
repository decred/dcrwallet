// Copyright (c) 2016 The Decred developers
// Licensed under the ISC license.  See LICENSE file in the project root for full license information.

using Paymetheus.Framework;
using System;
using System.Windows;
using System.Windows.Input;

namespace Paymetheus.ViewModels
{
    public sealed class ScriptsViewModel : ViewModelBase
    {
        public ICommand OpenImportScriptDialogCommand { get; }

        private void OpenImportScriptDialogAction()
        {
            var shell = (ShellViewModel)ViewModelLocator.ShellViewModel;
            shell.ShowDialog(new ImportScriptDialogViewModel(shell));
        }

        public ScriptsViewModel() : base()
        {
            OpenImportScriptDialogCommand = new DelegateCommand(OpenImportScriptDialogAction);
        }
    }
}
