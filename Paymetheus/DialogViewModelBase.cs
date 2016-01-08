// Copyright (c) 2016 The btcsuite developers
// Licensed under the ISC license.  See LICENSE file in the project root for full license information.

using System;
using System.Collections.Generic;
using System.Linq;
using System.Text;
using System.Threading.Tasks;
using System.Windows.Input;

namespace Paymetheus
{
    class DialogViewModelBase : ViewModelBase
    {
        public DialogViewModelBase(ShellViewModel shell) : base(shell)
        {
            HideDialogCommand = new DelegateCommand(HideDialog);
        }

        public ICommand HideDialogCommand { get; }

        public void HideDialog()
        {
            PostMessage(new HideDialogMessage());
        }
    }
}
